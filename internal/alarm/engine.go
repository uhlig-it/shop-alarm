package alarm

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/uhlig-it/shop-alarm/internal/config"
	"github.com/uhlig-it/shop-alarm/internal/metrics"
)

// topics derives all topic names from the config.
type topics struct {
	state      string
	cmd        string
	fault      string
	available  string
	events     string
	attributes string
	cmdPrefix  string // <root>/pir/# wildcard root
	audio      string // Frigate audio wildcard
}

// Notifier is the notification sink used by the engine (implemented by
// notify.Notifier; tests record the calls).
type Notifier interface {
	Notify(priority int, title, message, reviewID string)
}

// Engine wires the alarm Core to MQTT: subscriptions, retained publishes,
// deadline scheduling, Frigate profile verification and the flash/repeat
// loops. It implements alarm.Sink.
type Engine struct {
	cfg config.Config
	t   topics

	client   mqtt.Client
	core     *Core
	notifier Notifier
	metrics  *metrics.Recorder

	mu               sync.Mutex
	timers           map[string]*time.Timer
	cameraLossRun    bool
	bridgeLossRun    bool
	verifyPending    bool
	verifyAttempt    int
	verifyTimer      *time.Timer
	lastProfilePub   string // desired profile of the in-flight/last verification
	mismatchLastNtfy time.Time
	flashTicker      *time.Ticker
	repeatTicker     *time.Ticker
	flashOn          bool
	flashRed         bool
	stateInitialized bool
	ready            bool // machine effects allowed (after bootstrap)
	stateFileExists  bool
	profileState     string
	reviewID         string
	lastState        State
}

// NewEngine builds the engine; an existing state file is loaded up front.
func NewEngine(cfg config.Config, client mqtt.Client, core *Core, n Notifier, m *metrics.Recorder) *Engine {
	e := &Engine{
		cfg:      cfg,
		client:   client,
		core:     core,
		notifier: n,
		metrics:  m,
		timers:   make(map[string]*time.Timer),
		t: topics{
			state:      cfg.TopicRoot + "/alarm/state",
			cmd:        cfg.TopicRoot + "/alarm/cmnd",
			fault:      cfg.TopicRoot + "/alarm/fault",
			available:  cfg.TopicRoot + "/alarm/available",
			events:     cfg.TopicRoot + "/alarm/events",
			attributes: cfg.TopicRoot + "/alarm/attributes",
			cmdPrefix:  cfg.TopicRoot,
			audio:      cfg.FrigateAudioTopicPrefix,
		},
	}
	if d, err := LoadState(cfg.StateFile); err == nil {
		e.stateFileExists = true
		core.Restore(d)
	}
	if m != nil {
		m.SetFrigateAPIListener(e.onFrigateAPIDown)
	}
	return e
}

// onFrigateAPIDown runs from the stats poller goroutine whenever the Frigate
// API reachability flips; the loss rule re-evaluates and timers re-arm.
func (e *Engine) onFrigateAPIDown(down bool) {
	e.core.FrigateAPIUnreachable(down)
	e.supervision()
}

// Start runs after every MQTT (re)connect: subscribes first, then
// re-publishes availability and discovery, and reconciles after the
// retained-state settle. Subscriptions are established asynchronously so a
// missing SUBACK can never wedge startup, and no publishes happen while the
// subscription bursts are in flight.
func (e *Engine) Start() {
	e.subscribe()
	// Retained publishes go out at QoS 0: with clean-session=false and QoS 1,
	// paho's in-flight store misfires during the retained storm at connect
	// ("memorystore del: message N not found") and the retained state is
	// silently dropped — observed live 2026-09-21. Retained state is
	// republished on every transition anyway; mosquitto applies the retain
	// flag at any QoS.
	e.Publish(e.t.available, []byte("online"), 0, true)
	e.publishDiscovery()
	time.AfterFunc(e.cfg.ReconcileDelay, e.reconcile)
	e.syncTimers(time.Now())
}

// ---- subscribe ---------------------------------------------------------

func (e *Engine) subscribe() {
	stateTopic := strings.TrimSuffix(e.cfg.FrigateProfileTopic, "/set") + "/state"
	subs := []struct {
		topic string
		qos   byte
	}{
		{e.t.cmd, 1},
		{e.cfg.DoorTopic, 1},
		{e.t.cmdPrefix + "/pir/#", 1},
		{e.cfg.FrigateReviewsTopic, 1},
		{e.cfg.FrigateAvailableTopic, 1},
		{stateTopic, 0},
		{e.cfg.FrigateDetectStateTopic, 0},
		{e.cfg.FrigateRecordingsStateTopic, 0},
		{e.cfg.FrigateDetectStatusTopic, 0},
		{e.cfg.FrigateAudioTopicPrefix, 0},
		{e.cfg.BridgeStateTopic, 0},
	}
	for _, t := range e.cfg.SensorAvailableTopics {
		subs = append(subs, struct {
			topic string
			qos   byte
		}{t, 1})
	}
	for _, pattern := range e.cfg.DeviceLWTPatterns {
		subs = append(subs, struct {
			topic string
			qos   byte
		}{pattern, 0})
	}
	for _, s := range subs {
		tok := e.client.Subscribe(s.topic, s.qos, e.onMessage)
		go func(topic string, tok mqtt.Token) {
			tok.Wait()
			if tok.Error() != nil {
				slog.Error("subscribe failed", "topic", topic, "error", tok.Error())
			}
		}(s.topic, tok)
	}
}

// reconcile boots a fresh deployment into a safe initial state and then
// re-establishes the world (retained state, desired profile, verification).
func (e *Engine) reconcile() {
	e.mu.Lock()
	boot := !e.stateInitialized
	e.mu.Unlock()

	if boot {
		e.bootstrap()
	}
	e.core.Reconcile()
}

// bootstrap runs once per process: it decides the initial state from the
// retained evidence (or the persisted state file) and opens the gate for
// machine side effects. Until it runs, the retained-state storm only fills
// the core's memory; the engine drops every published side effect.
func (e *Engine) bootstrap() {
	e.mu.Lock()
	e.stateInitialized = true
	e.ready = true
	fileExists := e.stateFileExists
	profileArmed := e.profileState == "armed"
	e.mu.Unlock()

	if fileExists {
		return // state was restored at NewEngine; reconcile() re-establishes it
	}
	s := e.core.Summarize()
	evidence := profileArmed || (s.DetectOn != nil && *s.DetectOn) || (s.Recordings != nil && *s.Recordings)
	initial := e.core.InitialState(evidence)
	slog.Info("bootstrapping initial state", "state", initial, "evidence_armed", evidence)
	e.core.Restore(Data{State: initial, Updated: time.Now()})
}

// ---- alarm.Sink implementation -----------------------------------------

// Publish sends one MQTT message (sink interface). Never blocks: paho
// publishes asynchronously, and the error is reported on the token's
// completion so message handling can never stall the connection.
func (e *Engine) Publish(topic string, payload []byte, qos byte, retained bool) {
	tok := e.client.Publish(topic, qos, retained, payload)
	go func(topic string, tok mqtt.Token) {
		tok.Wait()
		if tok.Error() != nil {
			slog.Error("publish failed", "topic", topic, "error", tok.Error())
		}
	}(topic, tok)
}

// isReady reports whether the machine may publish side effects.
func (e *Engine) isReady() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.ready
}

func (e *Engine) StateChanged(d Data) {
	e.metrics.SetAlarmState(string(d.State))

	e.mu.Lock()
	e.lastState = d.State
	e.mu.Unlock()

	if !e.isReady() {
		// Retained-state storm before bootstrap: keep the last-known state
		// but publish nothing; bootstrap/reconcile republishes once open.
		slog.Debug("state change before bootstrap ignored", "state", d.State)
		return
	}

	if err := SaveState(e.cfg.StateFile, d); err != nil {
		slog.Error("state persist failed", "error", err)
	}
	e.Publish(e.t.state, []byte(string(d.State)), 0, true)

	e.syncTimers(time.Now())
	e.supervision()
	e.controlFlash(d)
}

func (e *Engine) FaultChanged(payload []byte) {
	if !e.isReady() {
		return
	}
	if payload == nil {
		payload = []byte{}
	}
	e.Publish(e.t.fault, payload, 0, true)
}

func (e *Engine) Event(kind string, details map[string]string) {
	if !e.isReady() {
		return
	}
	e.mu.Lock()
	state := e.lastState
	e.mu.Unlock()
	rec := map[string]any{
		"ts":    time.Now().Format(time.RFC3339),
		"type":  kind,
		"state": string(state),
	}
	if details != nil {
		rec["details"] = details
	}
	b, err := json.Marshal(rec)
	if err != nil {
		slog.Error("event marshal failed", "error", err)
		return
	}
	e.Publish(e.t.events, b, 1, false)
}

func (e *Engine) Attributes(a Attributes) {
	if !e.isReady() {
		return
	}
	b, err := json.Marshal(a)
	if err != nil {
		slog.Error("attributes marshal failed", "error", err)
		return
	}
	e.Publish(e.t.attributes, b, 0, true)
}

func (e *Engine) Notify(priority int, title, message string) {
	if !e.isReady() || e.notifier == nil {
		return
	}
	e.notifier.Notify(priority, title, message, e.lastReviewID())
}

func (e *Engine) Flash(on bool) {
	if !e.isReady() {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if on {
		e.startFlashLocked()
	} else {
		e.stopFlashLocked()
	}
}

func (e *Engine) ArmSequence() {
	if !e.isReady() {
		return
	}
	e.Publish(e.cfg.LightTopic, []byte("off"), 0, false)
	time.AfterFunc(2*time.Second, func() { e.Publish(e.cfg.PowerTopic, []byte("off"), 0, false) })
	e.Publish(e.cfg.Blink1Topic, []byte(`{"color":{"r":255,"g":0,"b":0}}`), 0, false)
	e.VerifyProfile()
}

func (e *Engine) DisarmSequence() {
	if !e.isReady() {
		return
	}
	e.Publish(e.cfg.PowerTopic, []byte("on"), 0, false)
	time.AfterFunc(2*time.Second, func() { e.Publish(e.cfg.RadioTopic, []byte("on"), 0, false) })
	e.Publish(e.cfg.Blink1Topic, []byte(`{"color":{"r":0,"g":255,"b":0}}`), 0, false)
	e.Publish(e.cfg.LightTopic, []byte("on"), 0, false)
	e.VerifyProfile()
}

// VerifyProfile re-publishes the desired Frigate profile and verifies the
// authoritative detect/recordings switch states against it. If a
// verification for the same desired profile is already in flight (e.g.
// the retained-state storm at connect re-triggered reconciliation), the
// publish is debounced and only the check is re-armed.
func (e *Engine) VerifyProfile() {
	if !e.isReady() {
		return
	}
	desired := "disarmed"
	if s := e.core.Summarize(); s.State != StateDisarmed {
		desired = "armed"
	}
	e.mu.Lock()
	pending := e.verifyPending
	last := e.lastProfilePub
	e.mu.Unlock()
	if pending && last == desired {
		e.scheduleVerifyCheck(e.cfg.VerifyInitialDelay)
		return
	}
	slog.Info("setting Frigate profile", "profile", desired)
	e.Publish(e.cfg.FrigateProfileTopic, []byte(desired), 1, false)
	e.mu.Lock()
	e.lastProfilePub = desired
	e.mu.Unlock()
	e.scheduleVerifyCheck(e.cfg.VerifyInitialDelay)
}

// ---- profile verification ---------------------------------------------

func (e *Engine) scheduleVerifyCheck(delay time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.verifyTimer != nil {
		e.verifyTimer.Stop()
	}
	e.verifyPending = true
	e.verifyTimer = time.AfterFunc(delay, e.verifyStep)
}

func (e *Engine) verifyStep() {
	s := e.core.Summarize()
	desired := s.State != StateDisarmed

	e.mu.Lock()
	attempt := e.verifyAttempt
	e.verifyPending = false
	e.verifyAttempt++
	e.verifyTimer = nil
	e.mu.Unlock()

	detect := s.DetectOn != nil && *s.DetectOn
	record := s.Recordings != nil && *s.Recordings
	known := s.DetectOn != nil && s.Recordings != nil

	if known && detect == desired && record == desired {
		e.verifyAttemptReset()
		e.core.ClearFault("profile_mismatch")
		slog.Info("Frigate profile verified", "profile", profileName(desired))
		return
	}
	if !known {
		// Frigate offline or still starting; supervision covers the loss.
		if attempt < 3 {
			e.scheduleVerifyCheck(e.cfg.VerifyRetryDelay)
		}
		return
	}
	if attempt >= 1 {
		// Fault in both directions: recordings on while disarmed is the
		// privacy regression this check exists for. The notification is
		// sent once per fault episode (and at most every
		// MismatchNotifyInterval) so a stuck state cannot spam.
		newlyFaulted := !e.core.HasFault("profile_mismatch")
		e.core.SetFault("profile_mismatch", map[string]any{
			"expected":   profileName(desired),
			"detect":     detect,
			"recordings": record,
		})
		e.Event("profile_mismatch", map[string]string{
			"expected": profileName(desired),
			"detect":   fmt.Sprintf("%v", detect),
			"record":   fmt.Sprintf("%v", record),
		})
		e.mu.Lock()
		lastNotify := e.mismatchLastNtfy
		e.mu.Unlock()
		if newlyFaulted || time.Since(lastNotify) >= e.cfg.MismatchNotifyInterval {
			e.mu.Lock()
			e.mismatchLastNtfy = time.Now()
			e.mu.Unlock()
			e.CoreNotifyFault(detect, record)
		}
		return
	}
	// Retry once: re-publish and check again shortly.
	e.Publish(e.cfg.FrigateProfileTopic, []byte(profileName(desired)), 1, false)
	e.scheduleVerifyCheck(e.cfg.VerifyRetryDelay)
}

// CoreNotifyFault notifies about a profile mismatch (kept out of the core
// because the verification lives in the engine).
func (e *Engine) CoreNotifyFault(detect, record bool) {
	if e.notifier == nil {
		return
	}
	e.notifier.Notify(4, "Werkstatt Frigate profile mismatch",
		fmt.Sprintf("Frigate did not follow the requested profile (detect=%v, recordings=%v).", detect, record),
		e.lastReviewID())
}

func (e *Engine) verifyAttemptReset() {
	e.mu.Lock()
	e.verifyAttempt = 0
	e.verifyPending = false
	e.mu.Unlock()
}

// ---- deadline timers ---------------------------------------------------

func (e *Engine) syncTimers(now time.Time) {
	d := e.core.Snapshot()
	e.mu.Lock()
	defer e.mu.Unlock()
	e.setTimerLocked("exit", d.ExitDeadline, now, func() { e.core.ExitExpired() })
	e.setTimerLocked("escalation", d.EscalationDeadline, now, func() { e.core.EscalationExpired() })
	e.setTimerLocked("entry", d.EntryDeadline, now, func() { e.core.EntryExpired() })
}

func (e *Engine) setTimerLocked(name string, deadline, now time.Time, fn func()) {
	if !HasDeadline(deadline) {
		if t, ok := e.timers[name]; ok {
			t.Stop()
			delete(e.timers, name)
		}
		return
	}
	delay := deadline.Sub(now)
	if delay < 0 {
		delay = 0
	}
	if t, ok := e.timers[name]; ok {
		t.Stop()
	}
	e.timers[name] = time.AfterFunc(delay, fn)
}

// supervision (re)arms the camera- and bridge-loss grace timers based on
// the current memory; called after state changes and after loss signals.
func (e *Engine) supervision() {
	s := e.core.Summarize()
	armedish := s.State == StateArmedAway || s.State == StatePending

	e.mu.Lock()
	defer e.mu.Unlock()

	if armedish && s.CameraLost {
		if !e.cameraLossRun {
			e.cameraLossRun = true
			e.timers["camera"] = time.AfterFunc(e.cfg.SupervisionCameraDelay, func() {
				e.core.FrigateLossExpired()
				e.supervision()
			})
		}
	} else {
		if t, ok := e.timers["camera"]; ok {
			t.Stop()
			delete(e.timers, "camera")
		}
		e.cameraLossRun = false
	}

	down := s.BridgeUp != nil && !*s.BridgeUp
	if armedish && down {
		if !e.bridgeLossRun {
			e.bridgeLossRun = true
			e.timers["bridge"] = time.AfterFunc(e.cfg.SupervisionBridgeDelay, func() {
				e.core.BridgeLossExpired()
				e.supervision()
			})
		}
	} else {
		if t, ok := e.timers["bridge"]; ok {
			t.Stop()
			delete(e.timers, "bridge")
		}
		e.bridgeLossRun = false
	}
}

// ---- flash / repeat loop ----------------------------------------------

func (e *Engine) controlFlash(d Data) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if d.State == StateTriggered && d.Flashing {
		e.startFlashLocked()
	} else {
		e.stopFlashLocked()
	}
}

func (e *Engine) startFlashLocked() {
	if e.flashOn {
		return
	}
	e.flashOn = true
	e.flashRed = true
	e.strobe()
	if e.cfg.FlashInterval > 0 {
		e.flashTicker = time.NewTicker(e.cfg.FlashInterval)
		go func() {
			for range e.flashTicker.C {
				e.mu.Lock()
				e.strobe()
				e.mu.Unlock()
			}
		}()
	}
	if e.cfg.TriggerRepeat > 0 {
		e.repeatTicker = time.NewTicker(e.cfg.TriggerRepeat)
		go func() {
			for range e.repeatTicker.C {
				e.repeatNotify()
			}
		}()
	}
}

func (e *Engine) stopFlashLocked() {
	if !e.flashOn {
		return
	}
	e.flashOn = false
	e.flashRed = false
	if e.flashTicker != nil {
		e.flashTicker.Stop()
		e.flashTicker = nil
	}
	if e.repeatTicker != nil {
		e.repeatTicker.Stop()
		e.repeatTicker = nil
	}
	e.Publish(e.cfg.Blink1Topic, []byte(`{"color":{"r":0,"g":0,"b":0}}`), 0, false)
	e.Publish(e.cfg.LightTopic, []byte("off"), 0, false)
}

func (e *Engine) strobe() {
	if e.flashRed {
		e.Publish(e.cfg.Blink1Topic, []byte(`{"color":{"r":255,"g":0,"b":0}}`), 0, false)
		e.Publish(e.cfg.LightTopic, []byte("on"), 0, false)
	} else {
		e.Publish(e.cfg.Blink1Topic, []byte(`{"color":{"r":0,"g":0,"b":0}}`), 0, false)
		e.Publish(e.cfg.LightTopic, []byte("off"), 0, false)
	}
	e.flashRed = !e.flashRed
}

func (e *Engine) repeatNotify() {
	s := e.core.Summarize()
	if s.State != StateTriggered || !s.Flashing {
		return
	}
	if e.notifier == nil {
		return
	}
	e.notifier.Notify(5, "Werkstatt alarm still active", "Triggered; send ACK on werkstatt/alarm/cmnd to silence.", e.lastReviewID())
}

// ---- message handling --------------------------------------------------

func (e *Engine) onMessage(_ mqtt.Client, msg mqtt.Message) {
	topic := msg.Topic()
	payload := string(msg.Payload())

	if e.isLWTTopic(topic) {
		e.metrics.SetDevice(deviceFromLWT(topic), strings.EqualFold(payload, "online"))
		return
	}

	switch {
	case topic == e.t.cmd:
		e.core.Command(payload)
	case topic == e.cfg.DoorTopic:
		e.core.Door(payload == "opened")
		e.metrics.SetDoor(payload == "opened")
	case strings.HasPrefix(topic, e.t.cmdPrefix+"/pir/"):
		e.core.Pir(topic, payload == "on")
	case topic == e.cfg.FrigateReviewsTopic:
		e.handleReview(payload)
	case topic == e.cfg.FrigateAvailableTopic:
		e.core.FrigateAvailable(payload)
		e.metrics.SetFrigateAvailable(payload == "online")
		e.supervision()
	case topic == e.cfg.FrigateDetectStateTopic:
		e.core.DetectState(payload == "ON")
		e.metrics.SetDetect(payload == "ON")
		e.reVerify()
	case topic == e.cfg.FrigateRecordingsStateTopic:
		e.core.RecordingsState(payload == "ON")
		e.metrics.SetRecordings(payload == "ON")
		e.reVerify()
	case topic == e.cfg.FrigateDetectStatusTopic:
		e.core.DetectStatus(payload == "online")
		e.metrics.SetCameraOnline(payload == "online")
		e.supervision()
	case topic == strings.TrimSuffix(e.cfg.FrigateProfileTopic, "/set")+"/state":
		e.mu.Lock()
		e.profileState = payload
		e.mu.Unlock()
		e.metrics.SetProfile(payload)
	case topic == e.cfg.BridgeStateTopic:
		e.core.BridgeState(payload == "1")
		e.metrics.SetBridge(payload == "1")
		e.supervision()
	case strings.HasPrefix(topic, e.cfg.FrigateAudioTopicPrefix):
		label := strings.TrimPrefix(topic, strings.TrimSuffix(e.cfg.FrigateAudioTopicPrefix, "#"))
		e.core.Audio(strings.Trim(label, "/"), payload == "ON")
	default:
		if e.isSensorAvailableTopic(topic) {
			e.core.SensorAvailable(topic, payload == "online")
		}
	}
}

// reVerify runs a verification step right after an authoritative switch
// state changes, so mismatches surface quickly.
func (e *Engine) reVerify() {
	e.mu.Lock()
	pending := e.verifyPending
	e.mu.Unlock()
	if pending {
		e.scheduleVerifyCheck(e.cfg.VerifyInitialDelay)
	}
}

func (e *Engine) isSensorAvailableTopic(topic string) bool {
	for _, t := range e.cfg.SensorAvailableTopics {
		if t == topic {
			return true
		}
	}
	return false
}

// isLWTTopic reports whether the topic matches one of the configured
// device-LWT subscription patterns.
func (e *Engine) isLWTTopic(topic string) bool {
	for _, pattern := range e.cfg.DeviceLWTPatterns {
		if mqttTopicMatches(pattern, topic) {
			return true
		}
	}
	return false
}

// mqttTopicMatches matches a concrete topic against an MQTT subscription
// pattern ("+" = one segment, "#" = the rest, must be last).
func mqttTopicMatches(pattern, topic string) bool {
	ps := strings.Split(pattern, "/")
	ts := strings.Split(topic, "/")
	for i, p := range ps {
		if p == "#" {
			return true
		}
		if i >= len(ts) {
			return false
		}
		if p != "+" && p != ts[i] {
			return false
		}
	}
	return len(ps) == len(ts)
}

// deviceFromLWT derives the device label from an LWT topic: strip the
// trailing "LWT" segment, then a trailing "tele" segment, then keep the
// last remaining segment — "tele/gosund-0/LWT" and
// "werkstatt/gosund-0/tele/LWT" both yield "gosund-0".
func deviceFromLWT(topic string) string {
	segs := strings.Split(topic, "/")
	if segs[len(segs)-1] != "LWT" {
		return topic
	}
	segs = segs[:len(segs)-1]
	if n := len(segs); n > 0 && strings.EqualFold(segs[n-1], "tele") {
		segs = segs[:n-1]
	}
	if len(segs) == 0 {
		return topic
	}
	return segs[len(segs)-1]
}

func (e *Engine) handleReview(payload string) {
	var ev struct {
		Type  string `json:"type"`
		After struct {
			ID       string   `json:"id"`
			Camera   string   `json:"camera"`
			Severity string   `json:"severity"`
			Objects  []string `json:"objects"`
		} `json:"after"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		slog.Warn("unparsable Frigate review", "error", err)
		return
	}
	if ev.After.ID != "" {
		e.mu.Lock()
		e.reviewID = ev.After.ID
		e.mu.Unlock()
	}
	e.core.Review(ev.After.ID, ev.After.Camera, ev.After.Severity, ev.After.Objects)
}

func (e *Engine) lastReviewID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.reviewID
}

// ---- HA discovery ------------------------------------------------------

func (e *Engine) publishDiscovery() {
	d := map[string]any{
		"name":                  "Werkstatt",
		"unique_id":             "werkstatt_alarm",
		"state_topic":           e.t.state,
		"command_topic":         e.t.cmd,
		"availability_topic":    e.t.available,
		"payload_available":     "online",
		"payload_not_available": "offline",
		"json_attributes_topic": e.t.attributes,
		"payload_arm_away":      CmdArmAway,
		"payload_disarm":        CmdDisarm,
		"supported_features":    []string{"arm_away"},
		"qos":                   1,
		"device": map[string]any{
			"identifiers":  []string{"werkstatt_alarm"},
			"name":         "Werkstatt",
			"manufacturer": "uhlig-it",
			"model":        "shop-alarm",
		},
	}
	b, err := json.Marshal(d)
	if err != nil {
		slog.Error("discovery marshal failed", "error", err)
		return
	}
	topic := e.cfg.DiscoveryPrefix + "/alarm_control_panel/werkstatt_alarm/config"
	e.Publish(topic, b, 0, true)
	slog.Info("published HA discovery", "topic", topic)
}

func profileName(armed bool) string {
	if armed {
		return "armed"
	}
	return "disarmed"
}
