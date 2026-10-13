package alarm

import (
	"testing"
	"time"

	"github.com/uhlig-it/shop-alarm/internal/config"
)

// recorder implements Sink for tests.
type recorder struct {
	states    []Data
	faults    [][]byte
	events    []string
	flashes   []bool
	attrs     []Attributes
	armSeq    int
	disarmSeq int
	verify    int
}

func (r *recorder) Publish(string, []byte, byte, bool)     {}
func (r *recorder) StateChanged(d Data)                    { r.states = append(r.states, d) }
func (r *recorder) FaultChanged(p []byte)                  { r.faults = append(r.faults, p) }
func (r *recorder) Event(kind string, _ map[string]string) { r.events = append(r.events, kind) }
func (r *recorder) Attributes(a Attributes)                { r.attrs = append(r.attrs, a) }
func (r *recorder) Flash(on bool)                          { r.flashes = append(r.flashes, on) }
func (r *recorder) ArmSequence()                           { r.armSeq++ }
func (r *recorder) DisarmSequence()                        { r.disarmSeq++ }
func (r *recorder) VerifyProfile()                         { r.verify++ }

func testClock(start time.Time) (func() time.Time, *time.Time) {
	t := start
	return func() time.Time { return t }, &t
}

func testCore(t *testing.T, into *recorder) (*Core, *time.Time) {
	t.Helper()
	cfg := config.Config{
		ExitDelay:              60 * time.Second,
		DoorEscalation:         60 * time.Second,
		EntryDelay:             30 * time.Second,
		PreArmSweep:            60 * time.Second,
		SupervisionBridgeDelay: 5 * time.Minute,
		FrigateCameraName:      "werkstatt",
	}
	now, clock := testClock(time.Date(2026, 9, 20, 20, 0, 0, 0, time.UTC))
	c := NewCore(cfg, into, now)
	return c, clock
}

func armedBaseline(c *Core) {
	c.FrigateAvailable("online")
	c.DetectState(true)
	c.RecordingsState(true)
	c.BridgeState(true)
	c.Door(false) // door closed
}

func TestArmHappyPathDoorClosed(t *testing.T) {
	r := &recorder{}
	c, clock := testCore(t, r)
	armedBaseline(c)

	c.Command(CmdArmAway)
	if got := c.Snapshot().State; got != StateArming {
		t.Fatalf("state = %s, want arming", got)
	}
	if r.armSeq != 1 {
		t.Fatalf("ArmSequence called %d times, want 1", r.armSeq)
	}
	if !HasDeadline(c.Snapshot().ExitDeadline) {
		t.Fatal("exit deadline not set")
	}

	// Door closes during the exit delay -> armed immediately.
	*clock = clock.Add(10 * time.Second)
	c.Door(false)
	if got := c.Snapshot().State; got != StateArmedAway {
		t.Fatalf("state = %s, want armed_away", got)
	}
	if HasDeadline(c.Snapshot().ExitDeadline) {
		t.Fatal("exit deadline still set after arming")
	}
}

func TestArmExitDelayDoorOpenEscalates(t *testing.T) {
	r := &recorder{}
	c, clock := testCore(t, r)
	armedBaseline(c)

	c.Command(CmdArmAway)
	c.Door(true) // door stays open during the exit delay
	*clock = clock.Add(60 * time.Second)
	c.ExitExpired()

	if got := c.Snapshot().State; got != StateArming {
		t.Fatalf("state = %s, want still arming", got)
	}
	if !HasDeadline(c.Snapshot().EscalationDeadline) {
		t.Fatal("escalation deadline not set")
	}
	if !hasEvent(r, "arming_door_open") {
		t.Fatalf("events = %v, want arming_door_open", r.events)
	}

	*clock = clock.Add(60 * time.Second)
	c.EscalationExpired()
	if got := c.Snapshot().State; got != StateTriggered {
		t.Fatalf("state = %s, want triggered", got)
	}
	if len(r.flashes) == 0 || !r.flashes[len(r.flashes)-1] {
		t.Fatal("flash not started")
	}
}

func TestArmPreconditionsRefuse(t *testing.T) {
	r := &recorder{}
	c, _ := testCore(t, r)

	// Frigate unknown, door unknown -> refuse.
	c.Command(CmdArmAway)
	if got := c.Snapshot().State; got != StateDisarmed {
		t.Fatalf("state = %s, want disarmed", got)
	}
	if len(r.faults) == 0 || len(r.faults[len(r.faults)-1]) == 0 {
		t.Fatal("fault not published")
	}
	if !hasEvent(r, "arm_failed") {
		t.Fatalf("events = %v, want arm_failed", r.events)
	}
}

func TestArmPreconditionsPIR(t *testing.T) {
	cfg := config.Config{
		ExitDelay:      60 * time.Second,
		DoorEscalation: 60 * time.Second,
		EntryDelay:     30 * time.Second,
		PreArmSweep:    60 * time.Second,
		PIRTopics:      []string{"werkstatt/pir/1"},
	}
	r := &recorder{}
	now, clock := testClock(time.Date(2026, 9, 20, 20, 0, 0, 0, time.UTC))
	c := NewCore(cfg, r, now)
	armedBaseline(c)

	// PIR recently tripped -> refuse.
	c.Pir("werkstatt/pir/1", true)
	*clock = clock.Add(10 * time.Second)
	// Reset so the state flows through the trip window check.
	c.Command(CmdArmAway)
	if got := c.Snapshot().State; got != StateDisarmed {
		t.Fatalf("state = %s, want disarmed after recent PIR trip", got)
	}

	// Trip older than the sweep window -> arm succeeds.
	*clock = clock.Add(2 * time.Minute)
	c.Pir("werkstatt/pir/1", false) // PIR clear again
	c.Command(CmdArmAway)
	if got := c.Snapshot().State; got != StateArming {
		t.Fatalf("state = %s, want arming after sweep window passed", got)
	}
}

func TestDoorOpenedTriggersPendingThenTriggered(t *testing.T) {
	r := &recorder{}
	c, clock := testCore(t, r)
	armedBaseline(c)
	c.Command(CmdArmAway)
	c.Door(false) // close during arming -> armed
	if got := c.Snapshot().State; got != StateArmedAway {
		t.Fatalf("state = %s, want armed_away", got)
	}

	*clock = clock.Add(5 * time.Minute)
	c.Door(true)
	if got := c.Snapshot().State; got != StatePending {
		t.Fatalf("state = %s, want pending", got)
	}
	if !HasDeadline(c.Snapshot().EntryDeadline) {
		t.Fatal("entry deadline not set")
	}

	*clock = clock.Add(30 * time.Second)
	c.EntryExpired()
	if got := c.Snapshot().State; got != StateTriggered {
		t.Fatalf("state = %s, want triggered", got)
	}
	if !c.Snapshot().Flashing {
		t.Fatal("flashing not set")
	}
}

func TestFireSkipsEntryDelay(t *testing.T) {
	r := &recorder{}
	c, _ := testCore(t, r)
	armedBaseline(c)
	c.Command(CmdArmAway)
	c.Door(false)

	c.Audio("fire_alarm", true)
	if got := c.Snapshot().State; got != StateTriggered {
		t.Fatalf("state = %s, want triggered directly", got)
	}
}

func TestFireWhileDisarmedEventsOnly(t *testing.T) {
	r := &recorder{}
	c, _ := testCore(t, r)
	c.Audio("smoke_detector", true)
	if got := c.Snapshot().State; got != StateDisarmed {
		t.Fatalf("state = %s, want disarmed", got)
	}
	if !hasEvent(r, "fire_disarmed") {
		t.Fatalf("events = %v, want fire_disarmed", r.events)
	}
}

func TestPersonReviewTriggers(t *testing.T) {
	r := &recorder{}
	c, clock := testCore(t, r)
	armedBaseline(c)
	c.Command(CmdArmAway)
	c.Door(false)

	c.Review("rev-1", "werkstatt", "alert", []string{"person"})
	if got := c.Snapshot().State; got != StatePending {
		t.Fatalf("state = %s, want pending", got)
	}
	// While disarmed (after disarm) reviews are ignored.
	c.Command(CmdDisarm)
	c.Review("rev-2", "werkstatt", "alert", []string{"person"})
	if got := c.Snapshot().State; got != StateDisarmed {
		t.Fatalf("state = %s, want disarmed", got)
	}
	// Non-person or non-alert reviews are ignored even when armed.
	c.Command(CmdArmAway)
	c.Review("rev-3", "werkstatt", "detection", []string{"dog"})
	if got := c.Snapshot().State; got != StateArming {
		t.Fatalf("state = %s, want arming (unrelated review ignored)", got)
	}
	_ = clock
}

func lastAttrs(r *recorder) Attributes {
	return r.attrs[len(r.attrs)-1]
}

func hasEvent(r *recorder, kind string) bool {
	for _, e := range r.events {
		if e == kind {
			return true
		}
	}
	return false
}

// review_id must describe the current alarm event: set by a person review and
// kept only for the entry-delay trigger that follows it. Regression for the
// 2026-10-13 incident, where a stale review_id from an earlier person review
// made the HA push label a door entry-delay alarm as "Person detected".
func TestReviewIDOnlyForPersonTriggers(t *testing.T) {
	r := &recorder{}
	c, _ := testCore(t, r)
	armedBaseline(c)
	c.Command(CmdArmAway)
	c.Door(false)

	c.Review("rev-person", "werkstatt", "alert", []string{"person"})
	if got := lastAttrs(r).ReviewID; got != "rev-person" {
		t.Fatalf("review_id after person review = %q, want rev-person", got)
	}

	// The person-triggered entry delay keeps the review id.
	c.EntryExpired()
	if got := c.Snapshot().State; got != StateTriggered {
		t.Fatalf("state = %s, want triggered", got)
	}
	if got := lastAttrs(r).ReviewID; got != "rev-person" {
		t.Fatalf("review_id after person trigger = %q, want rev-person", got)
	}

	// Re-arm clears it, so the following door alarm is not labelled a person.
	c.Command(CmdRearm)
	if got := lastAttrs(r).ReviewID; got != "" {
		t.Fatalf("review_id after rearm = %q, want empty", got)
	}
	c.Door(true)
	c.EntryExpired()
	if got := c.Snapshot().State; got != StateTriggered {
		t.Fatalf("state = %s, want triggered", got)
	}
	if got := lastAttrs(r).ReviewID; got != "" {
		t.Fatalf("review_id after door trigger = %q, want empty", got)
	}
}

// A direct non-person trigger (here: fire audio while a person review is still
// pending) must clear review_id, so no snapshot is attached to it either.
func TestReviewIDClearedByNonPersonTrigger(t *testing.T) {
	r := &recorder{}
	c, _ := testCore(t, r)
	armedBaseline(c)
	c.Command(CmdArmAway)
	c.Door(false)

	c.Review("rev-p", "werkstatt", "alert", []string{"person"})
	if got := lastAttrs(r).ReviewID; got != "rev-p" {
		t.Fatalf("review_id after person review = %q, want rev-p", got)
	}

	c.Audio("fire_alarm", true)
	if got := c.Snapshot().State; got != StateTriggered {
		t.Fatalf("state = %s, want triggered", got)
	}
	if got := lastAttrs(r).ReviewID; got != "" {
		t.Fatalf("review_id after fire trigger = %q, want empty", got)
	}
}

func TestAckSilencesButStaysTriggered(t *testing.T) {
	r := &recorder{}
	c, _ := testCore(t, r)
	armedBaseline(c)
	c.Command(CmdArmAway)
	c.Door(false)
	c.Door(true)
	c.EntryExpired()

	c.Command(CmdAck)
	d := c.Snapshot()
	if d.State != StateTriggered {
		t.Fatalf("state = %s, want triggered", d.State)
	}
	if d.Flashing {
		t.Fatal("flashing should be off after ACK")
	}
	if len(r.flashes) == 0 || r.flashes[len(r.flashes)-1] {
		t.Fatal("flash not stopped after ACK")
	}
}

func TestRearmReturnsToArmedAwayWithoutActors(t *testing.T) {
	r := &recorder{}
	c, _ := testCore(t, r)
	armedBaseline(c)
	c.Command(CmdArmAway)
	c.Door(false)
	c.Door(true)
	c.EntryExpired()

	c.Command(CmdRearm)
	d := c.Snapshot()
	if d.State != StateArmedAway {
		t.Fatalf("state = %s, want armed_away", d.State)
	}
	if d.Flashing {
		t.Fatal("flashing should be off after REARM")
	}
	if r.disarmSeq != 0 {
		t.Fatalf("DisarmSequence ran %d times, want 0 (REARM must not touch the actors)", r.disarmSeq)
	}
	if HasDeadline(d.ExitDeadline) || HasDeadline(d.EscalationDeadline) || HasDeadline(d.EntryDeadline) {
		t.Fatal("deadlines still set after REARM")
	}
	if HasDeadline(d.TriggeredAt) {
		t.Fatal("TriggeredAt still set after REARM")
	}
}

func TestRearmIgnoredUnlessTriggered(t *testing.T) {
	r := &recorder{}
	c, _ := testCore(t, r)
	armedBaseline(c)
	c.Command(CmdArmAway)
	c.Door(false) // armed_away, not triggered

	c.Command(CmdRearm)
	if got := c.Snapshot().State; got != StateArmedAway {
		t.Fatalf("state = %s, want armed_away (REARM must be a no-op)", got)
	}
	if r.disarmSeq != 0 {
		t.Fatalf("DisarmSequence ran %d times", r.disarmSeq)
	}
}

func TestDisarmFromAnyState(t *testing.T) {
	r := &recorder{}
	c, _ := testCore(t, r)
	armedBaseline(c)
	c.Command(CmdArmAway)
	c.Door(false)
	c.Door(true)
	c.EntryExpired()

	c.Command(CmdDisarm)
	d := c.Snapshot()
	if d.State != StateDisarmed {
		t.Fatalf("state = %s, want disarmed", d.State)
	}
	if r.disarmSeq != 1 {
		t.Fatalf("DisarmSequence called %d times, want 1", r.disarmSeq)
	}
	if HasDeadline(d.EntryDeadline) {
		t.Fatal("entry deadline still set")
	}
}

func TestSupervisionCameraLoss(t *testing.T) {
	r := &recorder{}
	c, clock := testCore(t, r)
	armedBaseline(c)
	c.Command(CmdArmAway)
	c.Door(false)

	// Frigate gone: MQTT offline AND API unreachable.
	c.FrigateAPIUnreachable(true)
	c.FrigateAvailable("offline")
	if got := c.Snapshot().State; got != StateArmedAway {
		t.Fatalf("state changed to %s, want armed_away", got)
	}
	// Fault published silently; the escalation is deferred so a flapping
	// stream cannot spam (decision 2026-09-21).
	if len(r.faults) == 0 || len(r.faults[len(r.faults)-1]) == 0 {
		t.Fatal("fault not published for frigate loss")
	}

	// After the grace period the loss triggers.
	*clock = clock.Add(31 * time.Second)
	c.FrigateLossExpired()
	if got := c.Snapshot().State; got != StateTriggered {
		t.Fatalf("state = %s, want triggered after camera loss grace", got)
	}

	// Recovery clears the fault.
	c.FrigateAvailable("online")
	if len(r.faults) == 0 || len(r.faults[len(r.faults)-1]) != 0 {
		t.Fatal("fault not cleared after recovery")
	}
}

// A stale retained frigate/available=offline (e.g. after a broker restart,
// when frigate reconnects without republishing availability) must NOT
// fault or escalate while the stream and API are healthy — this is
// the incident of 2026-09-21 (event storm).
func TestStaleAvailableOfflineIgnored(t *testing.T) {
	r := &recorder{}
	c, clock := testCore(t, r)
	armedBaseline(c)
	c.Command(CmdArmAway)
	c.Door(false)

	c.FrigateAvailable("offline") // retained storm delivery
	c.FrigateAvailable("offline") // repeated delivery
	if len(r.faults) > 0 && len(r.faults[len(r.faults)-1]) > 0 {
		t.Fatal("fault published for stale availability")
	}
	*clock = clock.Add(31 * time.Second)
	c.FrigateLossExpired()
	if got := c.Snapshot().State; got != StateArmedAway {
		t.Fatalf("state = %s, want armed_away (no escalation)", got)
	}
}

// Frigate gone for real: API polls fail, then the MQTT availability drops.
func TestCameraLossWhenFrigateGone(t *testing.T) {
	r := &recorder{}
	c, clock := testCore(t, r)
	armedBaseline(c)
	c.Command(CmdArmAway)
	c.Door(false)

	c.FrigateAPIUnreachable(true)
	c.FrigateAvailable("offline")
	c.FrigateAvailable("offline") // repeated delivery: still a single fault
	*clock = clock.Add(31 * time.Second)
	c.FrigateLossExpired()
	if got := c.Snapshot().State; got != StateTriggered {
		t.Fatalf("state = %s, want triggered", got)
	}

	// API comes back: fault clears, timers disarm.
	c.FrigateAPIUnreachable(false)
	if len(r.faults) == 0 || len(r.faults[len(r.faults)-1]) != 0 {
		t.Fatal("fault not cleared after API recovery")
	}
}

// A live stream-process loss is authoritative (API health irrelevant) and
// escalates after the grace period.
func TestStreamLossTriggersDirectly(t *testing.T) {
	r := &recorder{}
	c, clock := testCore(t, r)
	armedBaseline(c)
	c.Command(CmdArmAway)
	c.Door(false)

	c.DetectStatus(false)
	if len(r.faults) == 0 || len(r.faults[len(r.faults)-1]) == 0 {
		t.Fatal("no fault for stream loss")
	}

	*clock = clock.Add(31 * time.Second)
	c.FrigateLossExpired()
	if got := c.Snapshot().State; got != StateTriggered {
		t.Fatalf("state = %s, want triggered", got)
	}
}

func TestSupervisionCameraLossDisarmedIgnored(t *testing.T) {
	r := &recorder{}
	c, _ := testCore(t, r)
	armedBaseline(c) // sets frigate online etc.; state stays disarmed
	c.FrigateAvailable("offline")
	d := c.Snapshot()
	if d.State != StateDisarmed {
		t.Fatalf("state = %s, want disarmed", d.State)
	}
	if len(r.faults) > 0 && len(r.faults[len(r.faults)-1]) > 0 {
		t.Fatal("fault published while disarmed")
	}
}

func TestSupervisionBridge(t *testing.T) {
	r := &recorder{}
	c, clock := testCore(t, r)
	armedBaseline(c)
	c.Command(CmdArmAway)
	c.Door(false)

	c.BridgeState(false)
	if got := c.Snapshot().State; got != StateArmedAway {
		t.Fatalf("state = %s, want armed_away", got)
	}
	if len(r.faults) == 0 || len(r.faults[len(r.faults)-1]) == 0 {
		t.Fatal("fault not published for bridge loss")
	}

	*clock = clock.Add(5 * time.Minute)
	c.BridgeLossExpired()
	if got := c.Snapshot().State; got != StateTriggered {
		t.Fatalf("state = %s, want triggered after bridge grace", got)
	}

	// DISARM resolves the episode; the already-known bridge loss causes no
	// further fault change and no push.
	c.Command(CmdDisarm)
	c.BridgeState(false)
	if got := c.Snapshot().State; got != StateDisarmed {
		t.Fatalf("state = %s, want disarmed", got)
	}
}

func TestSensorOfflineRequiresBridgeUp(t *testing.T) {
	r := &recorder{}
	c, _ := testCore(t, r)
	armedBaseline(c)
	c.Command(CmdArmAway)
	c.Door(false)

	// Bridge down: sensor LWT going stale must NOT create a fault.
	c.BridgeState(false)
	c.SensorAvailable("mqtt-gpio-binary-sensor/mqtt-gpio-binary-sensor_shop/status", false)
	if len(r.faults) == 0 || len(r.faults[len(r.faults)-1]) == 0 {
		t.Fatal("expected no new fault while the bridge is down")
	}

	// Bridge back up + sensor offline -> fault.
	c.BridgeState(true)
	c.SensorAvailable("mqtt-gpio-binary-sensor/mqtt-gpio-binary-sensor_shop/status", false)
	if len(r.faults) == 0 || len(r.faults[len(r.faults)-1]) == 0 {
		t.Fatal("sensor offline while armed should fault")
	}
}

func TestSecondArmCommandWhileArmingIgnored(t *testing.T) {
	r := &recorder{}
	c, _ := testCore(t, r)
	armedBaseline(c)
	c.Command(CmdArmAway)
	seqs := r.armSeq
	c.Command(CmdArmAway)
	if c.Snapshot().State != StateArming {
		t.Fatalf("state = %s, want arming", c.Snapshot().State)
	}
	if r.armSeq != seqs {
		t.Fatal("ArmSequence ran twice")
	}
}

func TestRestorePreservesDeadlines(t *testing.T) {
	r := &recorder{}
	c, clock := testCore(t, r)
	armedBaseline(c)
	c.Command(CmdArmAway)
	c.Door(false)
	c.Door(true) // pending
	snap := c.Snapshot()

	// A fresh core loads the persisted state (deadline in the past).
	c2 := NewCore(c.Config(), &recorder{}, func() time.Time { return *clock })
	c2.Restore(snap)
	c2.EntryExpired() // deadline already past -> triggers
	if got := c2.Snapshot().State; got != StateTriggered {
		t.Fatalf("state = %s, want triggered after restore + past entry deadline", got)
	}
}

func TestInitialStateEvidence(t *testing.T) {
	r := &recorder{}
	c, _ := testCore(t, r)
	if got := c.InitialState(false); got != StateDisarmed {
		t.Fatalf("no evidence -> %s, want disarmed", got)
	}
	if got := c.InitialState(true); got != StateArmedAway {
		t.Fatalf("armed evidence -> %s, want armed_away", got)
	}
}
