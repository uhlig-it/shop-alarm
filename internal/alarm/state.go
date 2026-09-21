// Package alarm implements the workshop alarm state machine: states,
// transitions, arming preconditions, trigger fusion, supervision and the
// deadline bookkeeping that survives restarts via persisted timestamps.
package alarm

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/uhlig-it/shop-alarm/internal/config"
)

// State is the alarm state, published retained on <root>/alarm/state.
// The values are exactly the strings Home Assistant's MQTT alarm control
// panel accepts (verified against the 2026.9 integration docs); in particular
// there is no generic "armed" — the armed state is armed_away.
type State string

const (
	StateDisarmed  State = "disarmed"
	StateArming    State = "arming"
	StateArmedAway State = "armed_away"
	StatePending   State = "pending"
	StateTriggered State = "triggered"
)

// Commands accepted on <root>/alarm/cmnd (payloads match the HA MQTT
// alarm control panel configuration in DESIGN.md).
const (
	CmdArmAway = "ARM_AWAY"
	CmdDisarm  = "DISARM"
	CmdAck     = "ACK"
)

// Fault is one active supervision or precondition problem, published
// retained on <root>/alarm/fault (the oldest fault) and in full on the
// attributes topic.
type Fault struct {
	Reason  string         `json:"reason"`
	Since   time.Time      `json:"since"`
	Details map[string]any `json:"details,omitempty"`
}

// Data is the persisted alarm state. Deadlines are wall-clock timestamps
// so that a restart can reconstruct every timer exactly.
type Data struct {
	State       State     `json:"state"`
	Updated     time.Time `json:"updated"`
	TriggeredAt time.Time `json:"triggered_at,omitempty"`
	// ExitDeadline: arming -> armed (immediately when the door closes).
	ExitDeadline time.Time `json:"exit_deadline,omitempty"`
	// EscalationDeadline: arming with the door still open -> triggered.
	EscalationDeadline time.Time `json:"escalation_deadline,omitempty"`
	// EntryDeadline: pending -> triggered.
	EntryDeadline time.Time `json:"entry_deadline,omitempty"`
	// Flashing: triggered strobe/notification loop active (stopped by ACK).
	Flashing bool `json:"flashing,omitempty"`
}

// Attributes is the retained JSON published on <root>/alarm/attributes and
// exposed to Home Assistant as json_attributes of the alarm control panel.
type Attributes struct {
	State    string          `json:"state"`
	Since    time.Time       `json:"since,omitempty"`
	Door     string          `json:"door"` // closed | opened | unknown
	PIRs     map[string]bool `json:"pirs,omitempty"`
	Audio    map[string]bool `json:"audio,omitempty"`
	Faults   []Fault         `json:"faults,omitempty"`
	Profile  string          `json:"profile,omitempty"`
	ReviewID string          `json:"review_id,omitempty"`
}

// Sink receives the side effects of state transitions. Implementations must
// not call back into the core; the engine implementation (engine.go) is
// invoked after the core mutex is released.
type Sink interface {
	Publish(topic string, payload []byte, qos byte, retained bool)
	// StateChanged is called with the new state after every state change.
	StateChanged(d Data)
	// FaultChanged publishes the retained fault topic; nil payload clears.
	FaultChanged(payload []byte)
	// Event appends one audit record to <root>/alarm/events.
	Event(kind string, details map[string]string)
	// Attributes publishes the retained attributes JSON.
	Attributes(a Attributes)
	// Notify pushes a notification (ntfy backup channel; HA reacts to state).
	// priority follows the ntfy scale (1..5).
	Notify(priority int, title, message string)
	// Flash starts (on) or stops the triggered strobe and repeat loop.
	Flash(on bool)
	// ArmSequence executes the physical arm commands and arms Frigate.
	ArmSequence()
	// DisarmSequence executes the physical disarm commands and disarms Frigate.
	DisarmSequence()
	// VerifyProfile re-publishes the desired Frigate profile and verifies
	// the authoritative detect/recordings switch states against it.
	VerifyProfile()
}

// Core is the alarm state machine. All methods are safe for concurrent use;
// side effects are delivered to the Sink after the internal lock is released.
type Core struct {
	mu  sync.Mutex
	now func() time.Time
	cfg config.Config
	out Sink

	data Data

	// Last-known sensor state (memory reconstructed from retained topics).
	doorKnown bool
	doorOpen  bool
	pirState  map[string]bool
	pirTrip   map[string]time.Time

	frigateAvailable    string // "", "online", "stopped", "offline"
	frigateDetectStatus *bool  // stream-process health (nil = unknown)
	frigateAPIDown      *bool  // Frigate /api/stats unreachable (nil = unknown)
	detectOn            *bool  // authoritative switch signal
	recordingsOn        *bool
	bridgeUp            *bool
	sensorOnline        map[string]bool
	audioOn             map[string]bool
	lastReviewID        string

	faults map[string]Fault
	acked  bool
}

// NewCore returns the state machine. now is injectable for tests.
func NewCore(cfg config.Config, out Sink, now func() time.Time) *Core {
	if now == nil {
		now = time.Now
	}
	return &Core{
		now:          now,
		cfg:          cfg,
		out:          out,
		data:         Data{State: StateDisarmed},
		pirState:     make(map[string]bool),
		pirTrip:      make(map[string]time.Time),
		sensorOnline: make(map[string]bool),
		audioOn:      make(map[string]bool),
		faults:       make(map[string]Fault),
	}
}

// commit runs fn under the lock; fn appends delayed side effects to acts,
// which run after the lock is released (sinks may call back into the core).
// Without a sink (NewEngine restores the persisted state before main.go
// installs the engine as sink) the acts are dropped: reconcile() replays
// them on connect.
func (c *Core) commit(fn func(acts *[]func())) {
	var acts []func()
	c.mu.Lock()
	fn(&acts)
	hasSink := c.out != nil
	c.mu.Unlock()
	if !hasSink {
		return
	}
	for _, act := range acts {
		act()
	}
}

// SetSink installs (or replaces) the side-effect sink.
func (c *Core) SetSink(out Sink) {
	c.mu.Lock()
	c.out = out
	c.mu.Unlock()
}

// Restore loads persisted state. Supervision loss deadlines are dropped:
// their grace period restarts from the live retained topics after startup.
func (c *Core) Restore(d Data) {
	c.commit(func(acts *[]func()) {
		c.data = d
		*acts = append(*acts, c.stateChangedAct(d))
	})
}

// Config returns the config the machine was created with.
func (c *Core) Config() config.Config {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg
}

// Snapshot returns the current persisted state.
func (c *Core) Snapshot() Data {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.data
}

func (c *Core) armedish() bool {
	s := c.data.State
	return s == StateArmedAway || s == StatePending
}

// desiredProfile derives the Frigate profile that matches the current state.
func (c *Core) desiredProfile() string {
	if c.data.State == StateDisarmed {
		return "disarmed"
	}
	return "armed"
}

func (c *Core) cameraLost() bool {
	// Live stream-process loss is authoritative: frigate publishes
	// status/detect only on real process state changes.
	if c.frigateDetectStatus != nil && !*c.frigateDetectStatus {
		return true
	}
	// frigate/available offline alone is NOT loss: the topic goes stale
	// (retained "offline") whenever the broker restarts and frigate's
	// reconnect does not republish the availability. Only trust it when the
	// Frigate API is also unreachable (fresh, opus-side signal).
	switch c.frigateAvailable {
	case "stopped", "offline":
		return c.frigateAPIDown != nil && *c.frigateAPIDown
	}
	return false
}

func (c *Core) faultPayloadLocked() []byte {
	if len(c.faults) == 0 {
		return nil
	}
	fs := make([]Fault, 0, len(c.faults))
	for _, f := range c.faults {
		fs = append(fs, f)
	}
	sort.Slice(fs, func(i, j int) bool { return fs[i].Since.Before(fs[j].Since) })
	b, err := json.Marshal(fs[0])
	if err != nil {
		return nil
	}
	return b
}

func (c *Core) setFaultLocked(acts *[]func(), reason string, details map[string]any) {
	if _, ok := c.faults[reason]; !ok {
		c.faults[reason] = Fault{Reason: reason, Since: c.now(), Details: details}
		detail := details
		*acts = append(*acts, func() {
			m := map[string]string{"reason": reason}
			if detail != nil {
				if b, err := json.Marshal(detail); err == nil {
					m["details"] = string(b)
				}
			}
			c.out.Event("fault", m)
		})
	}
	payload := c.faultPayloadLocked()
	*acts = append(*acts, func() { c.out.FaultChanged(payload) })
}

func (c *Core) clearFaultLocked(acts *[]func(), reason string) {
	if _, ok := c.faults[reason]; ok {
		delete(c.faults, reason)
		*acts = append(*acts, func() { c.out.Event("fault_cleared", map[string]string{"reason": reason}) })
	}
	payload := c.faultPayloadLocked()
	*acts = append(*acts, func() { c.out.FaultChanged(payload) })
}

func (c *Core) attributesLocked() Attributes {
	a := Attributes{
		State:    string(c.data.State),
		Since:    c.data.Updated,
		Profile:  c.desiredProfile(),
		ReviewID: c.lastReviewID,
		PIRs:     map[string]bool{},
		Audio:    map[string]bool{},
		Faults:   make([]Fault, 0, len(c.faults)),
	}
	if !c.doorKnown {
		a.Door = "unknown"
	} else if c.doorOpen {
		a.Door = "opened"
	} else {
		a.Door = "closed"
	}
	for k, v := range c.pirState {
		a.PIRs[k] = v
	}
	for k, v := range c.audioOn {
		a.Audio[k] = v
	}
	for _, f := range c.faults {
		a.Faults = append(a.Faults, f)
	}
	sort.Slice(a.Faults, func(i, j int) bool { return a.Faults[i].Since.Before(a.Faults[j].Since) })
	return a
}

func (c *Core) attributesAct() func() {
	a := c.attributesLocked()
	return func() { c.out.Attributes(a) }
}

func (c *Core) stateChangedAct(d Data) func() {
	return func() { c.out.StateChanged(d) }
}

func (c *Core) setStateLocked(acts *[]func(), s State) {
	c.data.State = s
	c.data.Updated = c.now()
	*acts = append(*acts, c.stateChangedAct(c.data))
	*acts = append(*acts, c.attributesAct())
}

// Command handles <root>/alarm/cmnd payloads: ARM_AWAY, DISARM, ACK.
func (c *Core) Command(cmd string) {
	c.commit(func(acts *[]func()) {
		switch cmd {
		case CmdArmAway:
			*acts = append(*acts, func() { c.out.Event("command", map[string]string{"command": cmd}) })
			c.arm(acts)
		case CmdDisarm:
			*acts = append(*acts, func() { c.out.Event("command", map[string]string{"command": cmd}) })
			c.disarm(acts, "command")
		case CmdAck:
			*acts = append(*acts, func() { c.out.Event("command", map[string]string{"command": cmd}) })
			c.ack(acts)
		default:
			*acts = append(*acts, func() { c.out.Event("command", map[string]string{"command": cmd, "result": "unknown"}) })
		}
	})
}

func (c *Core) arm(acts *[]func()) {
	if c.data.State != StateDisarmed {
		*acts = append(*acts, func() {
			c.out.Event("arm_failed", map[string]string{"reason": "not_disarmed", "state": string(c.data.State)})
		})
		return
	}

	now := c.now()
	var problems []string
	if c.frigateAvailable != "online" {
		problems = append(problems, "Frigate is not online")
	}
	if !c.doorKnown {
		problems = append(problems, "door state unknown")
	}
	for _, pir := range c.cfg.PIRTopics {
		if _, known := c.pirState[pir]; !known {
			problems = append(problems, "PIR state unknown: "+pir)
			continue
		}
		if c.pirState[pir] {
			problems = append(problems, "PIR motion: "+pir)
		}
		if t, ok := c.pirTrip[pir]; ok && now.Sub(t) < c.cfg.PreArmSweep {
			problems = append(problems, "PIR tripped recently: "+pir)
		}
	}

	if len(problems) > 0 {
		joined := strings.Join(problems, "; ")
		c.setFaultLocked(acts, "preconditions", map[string]any{"problems": problems})
		*acts = append(*acts, func() { c.out.Notify(4, "Arming refused", joined) })
		*acts = append(*acts, func() { c.out.Event("arm_failed", map[string]string{"reason": joined}) })
		return
	}

	c.clearFaultLocked(acts, "preconditions")
	c.setStateLocked(acts, StateArming)
	c.data.ExitDeadline = now.Add(c.cfg.ExitDelay)
	c.data.EscalationDeadline = time.Time{}
	delay := c.cfg.ExitDelay
	*acts = append(*acts, func() {
		c.out.Event("transition", map[string]string{"to": string(StateArming), "exit_delay": delay.String()})
	})
	*acts = append(*acts, func() { c.out.ArmSequence() })
}

func (c *Core) disarm(acts *[]func(), source string) {
	c.acked = false
	c.clearFaultLocked(acts, "preconditions")
	c.data.ExitDeadline = time.Time{}
	c.data.EscalationDeadline = time.Time{}
	c.data.EntryDeadline = time.Time{}
	c.data.Flashing = false
	*acts = append(*acts, func() { c.out.Flash(false) })
	c.setStateLocked(acts, StateDisarmed)
	*acts = append(*acts, func() { c.out.Event("transition", map[string]string{"to": string(StateDisarmed), "source": source}) })
	*acts = append(*acts, func() { c.out.DisarmSequence() })
}

func (c *Core) ack(acts *[]func()) {
	if c.data.State != StateTriggered {
		return
	}
	c.acked = true
	c.data.Flashing = false
	c.setStateLocked(acts, StateTriggered)
	*acts = append(*acts, func() { c.out.Event("ack", nil) })
	*acts = append(*acts, func() { c.out.Flash(false) })
}

// Door updates the reed-contact state (opened/closed).
func (c *Core) Door(opened bool) {
	c.commit(func(acts *[]func()) {
		c.doorKnown = true
		c.doorOpen = opened
		*acts = append(*acts, c.attributesAct())

		switch c.data.State {
		case StateArming:
			if !opened {
				// Door closed during the exit delay: armed immediately.
				c.data.ExitDeadline = time.Time{}
				c.data.EscalationDeadline = time.Time{}
				c.setStateLocked(acts, StateArmedAway)
				*acts = append(*acts, func() {
					c.out.Event("transition", map[string]string{"to": string(StateArmedAway), "source": "door_closed"})
				})
			}
		case StateArmedAway:
			if opened {
				c.enterPending(acts, "door")
			}
		case StatePending:
			if opened {
				*acts = append(*acts, func() { c.out.Event("trigger", map[string]string{"source": "door", "skipped": "pending"}) })
			}
		}
	})
}

// Pir updates one PIR sensor (topic is the full <prefix>/pir/<n> topic).
func (c *Core) Pir(topic string, on bool) {
	c.commit(func(acts *[]func()) {
		c.pirState[topic] = on
		if on {
			c.pirTrip[topic] = c.now()
		}
		*acts = append(*acts, c.attributesAct())
		if c.data.State == StateArmedAway && on {
			c.enterPending(acts, "pir")
		}
	})
}

// Review handles Frigate review-alert events (frigate/reviews).
func (c *Core) Review(id, camera, severity string, objects []string) {
	if severity != "alert" || (camera != "" && !strings.EqualFold(camera, "werkstatt")) {
		return
	}
	hasPerson := false
	for _, o := range objects {
		if strings.EqualFold(o, "person") {
			hasPerson = true
			break
		}
	}
	if !hasPerson {
		return
	}
	c.commit(func(acts *[]func()) {
		c.lastReviewID = id
		*acts = append(*acts, c.attributesAct())
		switch c.data.State {
		case StateArmedAway:
			c.enterPending(acts, "person")
		case StatePending:
			*acts = append(*acts, func() { c.out.Event("trigger", map[string]string{"source": "person", "skipped": "pending"}) })
		}
	})
}

// Audio handles Frigate audio-label messages: fire alarms trigger directly
// (no entry delay) and matter even while disarmed.
func (c *Core) Audio(label string, on bool) {
	c.commit(func(acts *[]func()) {
		if on {
			c.audioOn[label] = true
		} else {
			delete(c.audioOn, label)
		}
		*acts = append(*acts, c.attributesAct())
		if !on || (label != "fire_alarm" && label != "smoke_detector") {
			return
		}
		switch c.data.State {
		case StateArmedAway, StatePending:
			c.trigger(acts, "fire")
		case StateDisarmed:
			*acts = append(*acts, func() { c.out.Event("fire_disarmed", map[string]string{"label": label}) })
			*acts = append(*acts, func() {
				c.out.Notify(5, "Fire or smoke alarm detected", "Audio detection reports "+label+" in the workshop.")
			})
		}
	})
}

func (c *Core) enterPending(acts *[]func(), source string) {
	c.data.EntryDeadline = c.now().Add(c.cfg.EntryDelay)
	delay := c.cfg.EntryDelay
	c.setStateLocked(acts, StatePending)
	*acts = append(*acts, func() { c.out.Event("trigger", map[string]string{"source": source, "entry_delay": delay.String()}) })
}

func (c *Core) trigger(acts *[]func(), source string) {
	c.data.EntryDeadline = time.Time{}
	c.data.Flashing = true
	c.setStateLocked(acts, StateTriggered)
	c.data.TriggeredAt = c.data.Updated
	*acts = append(*acts, func() { c.out.Event("trigger", map[string]string{"source": source}) })
	*acts = append(*acts, func() { c.out.Flash(true) })
	*acts = append(*acts, func() { c.out.Notify(5, "Werkstatt alarm triggered", "Source: "+source) })
}

// ExitExpired fires the exit-delay deadline.
func (c *Core) ExitExpired() {
	c.commit(func(acts *[]func()) {
		if c.data.State != StateArming {
			return
		}
		if c.doorKnown && !c.doorOpen {
			c.data.ExitDeadline = time.Time{}
			c.setStateLocked(acts, StateArmedAway)
			*acts = append(*acts, func() {
				c.out.Event("transition", map[string]string{"to": string(StateArmedAway), "source": "exit_delay"})
			})
			return
		}
		// Door still open (or unknown, e.g. the bridge is down): keep arming,
		// notify, and escalate to triggered after the agreed grace period.
		c.data.ExitDeadline = time.Time{}
		c.data.EscalationDeadline = c.now().Add(c.cfg.DoorEscalation)
		delay := c.cfg.DoorEscalation
		c.setStateLocked(acts, StateArming)
		*acts = append(*acts, func() { c.out.Event("arming_door_open", map[string]string{"escalation_delay": delay.String()}) })
		*acts = append(*acts, func() {
			c.out.Notify(3, "Werkstatt still arming", "The door is still open (or its state is unknown). Arming continues.")
		})
	})
}

// EscalationExpired fires the "door still open" escalation deadline.
func (c *Core) EscalationExpired() {
	c.commit(func(acts *[]func()) {
		if c.data.State != StateArming {
			return
		}
		c.data.EscalationDeadline = time.Time{}
		c.trigger(acts, "door_open")
	})
}

// EntryExpired fires the entry-delay deadline: pending -> triggered.
func (c *Core) EntryExpired() {
	c.commit(func(acts *[]func()) {
		if c.data.State != StatePending {
			return
		}
		c.trigger(acts, "entry_delay")
	})
}

// FrigateAvailable tracks frigate/available (online | stopped | offline).
func (c *Core) FrigateAvailable(a string) {
	c.commit(func(acts *[]func()) {
		c.frigateAvailable = a
		switch a {
		case "online":
			c.clearFaultLocked(acts, "frigate_loss")
			c.reconcileLocked(acts)
		case "stopped", "offline":
			c.frigateLossEvent(acts)
		}
	})
}

// DetectStatus tracks frigate/<camera>/status/detect (stream-process health).
func (c *Core) DetectStatus(online bool) {
	c.commit(func(acts *[]func()) {
		c.frigateDetectStatus = &online
		if c.cameraLost() {
			c.frigateLossEvent(acts)
		} else {
			c.clearFaultLocked(acts, "frigate_loss")
		}
	})
}

// DetectState / RecordingsState track the authoritative switch signals.
func (c *Core) DetectState(on bool) {
	c.commit(func(acts *[]func()) {
		c.detectOn = &on
	})
}

func (c *Core) RecordingsState(on bool) {
	c.commit(func(acts *[]func()) {
		c.recordingsOn = &on
	})
}

func (c *Core) frigateLossEvent(acts *[]func()) {
	if c.data.State == StateArming || c.data.State == StateDisarmed {
		return // supervision applies while armed/pending only
	}
	if !c.cameraLost() {
		return // available-offline alone is a stale LWT, not loss
	}
	// Notify once per episode: repeated deliveries of the same loss signal
	// (e.g. the retained storm at connect) must not re-notify.
	if _, ok := c.faults["frigate_loss"]; !ok {
		c.setFaultLocked(acts, "frigate_loss", map[string]any{
			"frigate_available":    c.frigateAvailable,
			"detect_status_online": c.frigateDetectStatus != nil && *c.frigateDetectStatus,
			"api_down":             c.frigateAPIDown != nil && *c.frigateAPIDown,
		})
		*acts = append(*acts, func() {
			c.out.Notify(4, "Supervision: camera lost", "Frigate or the shop camera stream is down while armed.")
		})
	}
}

// BridgeState tracks $SYS/broker/connection/<remote>/state on opus (1/0).
func (c *Core) BridgeState(up bool) {
	c.commit(func(acts *[]func()) {
		if c.bridgeUp != nil && *c.bridgeUp == up {
			c.bridgeUp = &up
			return
		}
		c.bridgeUp = &up
		if up {
			c.clearFaultLocked(acts, "bridge_down")
			return
		}
		if c.armedish() {
			c.setFaultLocked(acts, "bridge_down", nil)
			*acts = append(*acts, func() { c.out.Event("supervision", map[string]string{"signal": "bridge", "value": "0"}) })
			*acts = append(*acts, func() {
				c.out.Notify(4, "Supervision: shop bridge down", "The MQTT bridge to the shop is down while armed.")
			})
		}
	})
}

// SensorAvailable tracks the LWT availability of shop-side sensors
// (door, PIRs). Offline only counts while the bridge is up: once the
// bridge is gone, shop-side LWT topics go stale and are untrustworthy.
func (c *Core) SensorAvailable(topic string, online bool) {
	c.commit(func(acts *[]func()) {
		c.sensorOnline[topic] = online
		if online || !c.armedish() {
			return
		}
		up := c.bridgeUp == nil || *c.bridgeUp
		if !up {
			return
		}
		c.setFaultLocked(acts, "sensor_offline", map[string]any{"topic": topic})
		*acts = append(*acts, func() {
			c.out.Event("supervision", map[string]string{"signal": "sensor_lwt", "topic": topic, "value": "offline"})
		})
		*acts = append(*acts, func() {
			c.out.Notify(4, "Supervision: sensor offline", "A shop-side sensor (door/PIR) lost its LWT while armed.")
		})
	})
}

// FrigateLossExpired fires after the camera-loss grace period while armed.
func (c *Core) FrigateLossExpired() {
	c.commit(func(acts *[]func()) {
		if !c.armedish() || !c.cameraLost() {
			return
		}
		c.trigger(acts, "camera_loss")
	})
}

// BridgeLossExpired fires after the bridge-loss grace period while armed.
func (c *Core) BridgeLossExpired() {
	c.commit(func(acts *[]func()) {
		if !c.armedish() || c.bridgeUp == nil || *c.bridgeUp {
			return
		}
		c.trigger(acts, "bridge_loss")
	})
}

// FrigateAPIUnreachable tracks whether the Frigate API (fresh, opus-side
// signal) is reachable. Down-transitions re-evaluate the camera-loss
// condition; recovery clears the fault.
func (c *Core) FrigateAPIUnreachable(down bool) {
	c.commit(func(acts *[]func()) {
		old := c.frigateAPIDown != nil && *c.frigateAPIDown
		if old == down {
			return
		}
		c.frigateAPIDown = &down
		if down {
			c.frigateLossEvent(acts)
		} else {
			c.clearFaultLocked(acts, "frigate_loss")
			*acts = append(*acts, c.attributesAct())
		}
	})
}

// Reconcile re-establishes the world after (re)connect: republishes the
// retained state, the desired Frigate profile and re-verifies the privacy
// floor. Called after the retained-state settle delay.
func (c *Core) Reconcile() {
	c.commit(func(acts *[]func()) {
		c.reconcileLocked(acts)
	})
}

func (c *Core) reconcileLocked(acts *[]func()) {
	*acts = append(*acts, c.stateChangedAct(c.data))
	*acts = append(*acts, c.attributesAct())
	*acts = append(*acts, func() { c.out.VerifyProfile() })
	*acts = append(*acts, func() { c.out.Event("reconcile", map[string]string{"state": string(c.data.State)}) })
}

// SetFault/ClearFault let the engine report problems it detected itself
// (e.g. a Frigate profile switch that the authoritative states contradict).
func (c *Core) SetFault(reason string, details map[string]any) {
	c.commit(func(acts *[]func()) {
		c.setFaultLocked(acts, reason, details)
		*acts = append(*acts, c.attributesAct())
	})
}

func (c *Core) ClearFault(reason string) {
	c.commit(func(acts *[]func()) {
		c.clearFaultLocked(acts, reason)
		*acts = append(*acts, c.attributesAct())
	})
}

// HasFault reports whether a fault with that reason is currently active.
func (c *Core) HasFault(reason string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.faults[reason]
	return ok
}

// Summary exposes a read-only view for the engine.
type Summary struct {
	State        State
	Flashing     bool
	DetectOn     *bool
	Recordings   *bool
	CameraLost   bool
	BridgeUp     *bool
	FrigateUp    bool
	FrigateKnown bool
}

// Summarize returns a read-only snapshot of the machine for the engine.
func (c *Core) Summarize() Summary {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Summary{
		State:        c.data.State,
		Flashing:     c.data.Flashing,
		DetectOn:     c.detectOn,
		Recordings:   c.recordingsOn,
		CameraLost:   c.cameraLost(),
		BridgeUp:     c.bridgeUp,
		FrigateUp:    c.frigateAvailable == "online",
		FrigateKnown: c.frigateAvailable != "",
	}
}

// InitialState returns the state a fresh deployment boots into: evidence of
// a currently armed system (retained profile/switch states) keeps it armed
// so that a first start never silently disarms a live alarm.
func (c *Core) InitialState(evidenceArmed bool) State {
	c.mu.Lock()
	defer c.mu.Unlock()
	if evidenceArmed {
		c.data.State = StateArmedAway
		c.data.Updated = c.now()
		return StateArmedAway
	}
	return StateDisarmed
}
