package alarm

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/uhlig-it/shop-alarm/internal/config"
	"github.com/uhlig-it/shop-alarm/internal/metrics"
)

// ---- fake MQTT client ----------------------------------------------------

type fakePub struct {
	topic    string
	payload  string
	qos      byte
	retained bool
}

type fakeToken struct{}

func (fakeToken) Wait() bool                     { return true }
func (fakeToken) WaitTimeout(time.Duration) bool { return true }
func (fakeToken) Error() error                   { return nil }
func (fakeToken) Done() <-chan struct{}          { return nil }

type fakeClient struct {
	mu   sync.Mutex
	subs map[string]mqtt.MessageHandler
	pubs []fakePub
}

func newFakeClient() *fakeClient {
	return &fakeClient{subs: make(map[string]mqtt.MessageHandler)}
}

func (c *fakeClient) IsConnected() bool      { return true }
func (c *fakeClient) IsConnectionOpen() bool { return true }
func (c *fakeClient) Connect() mqtt.Token    { return fakeToken{} }
func (c *fakeClient) Disconnect(uint)        {}
func (c *fakeClient) OptionsReader() mqtt.ClientOptionsReader {
	return mqtt.ClientOptionsReader{}
}
func (c *fakeClient) Publish(topic string, qos byte, retained bool, payload any) mqtt.Token {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pubs = append(c.pubs, fakePub{topic, string(payload.([]byte)), qos, retained})
	return fakeToken{}
}
func (c *fakeClient) Subscribe(topic string, qos byte, cb mqtt.MessageHandler) mqtt.Token {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subs[topic] = cb
	return fakeToken{}
}
func (c *fakeClient) SubscribeMultiple(filters map[string]byte, cb mqtt.MessageHandler) mqtt.Token {
	for t := range filters {
		c.subs[t] = cb
	}
	return fakeToken{}
}
func (c *fakeClient) Unsubscribe(topics ...string) mqtt.Token { return fakeToken{} }
func (c *fakeClient) AddRoute(string, mqtt.MessageHandler)    {}

type fakeMessage struct {
	topic   string
	payload string
}

func (m fakeMessage) Duplicate() bool   { return false }
func (m fakeMessage) Qos() byte         { return 0 }
func (m fakeMessage) Retained() bool    { return false }
func (m fakeMessage) Topic() string     { return m.topic }
func (m fakeMessage) MessageID() uint16 { return 0 }
func (m fakeMessage) Payload() []byte   { return []byte(m.payload) }
func (m fakeMessage) Ack()              {}

// deliver simulates an incoming broker message (retained or live).
func (c *fakeClient) deliver(topic, payload string) {
	c.mu.Lock()
	var patterns []string
	handlers := make([]mqtt.MessageHandler, 0)
	for p, h := range c.subs {
		if mqttTopicMatches(p, topic) {
			patterns = append(patterns, p)
			handlers = append(handlers, h)
		}
	}
	c.mu.Unlock()
	for _, h := range handlers {
		h(nil, fakeMessage{topic, payload})
	}
}

func (c *fakeClient) pubsOn(topic string) []fakePub {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []fakePub
	for _, p := range c.pubs {
		if p.topic == topic {
			out = append(out, p)
		}
	}
	return out
}

func (c *fakeClient) pubCount(topic string) int { return len(c.pubsOn(topic)) }

// recordingNotifier captures engine notifications.
type recordingNotifier struct {
	mu    sync.Mutex
	times []string
}

func (n *recordingNotifier) Notify(_ int, title, _, _ string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.times = append(n.times, title)
}

func (n *recordingNotifier) count(title string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	c := 0
	for _, t := range n.times {
		if t == title {
			c++
		}
	}
	return c
}

// ---- helpers --------------------------------------------------------------

func testEngineConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		TopicRoot:                   "werkstatt",
		BrokerURL:                   "tcp://localhost:1883",
		ClientID:                    "shop-alarm-test",
		FrigateProfileTopic:         "frigate/profile/set",
		FrigateReviewsTopic:         "frigate/reviews",
		FrigateAvailableTopic:       "frigate/available",
		FrigateDetectStateTopic:     "frigate/werkstatt/detect/state",
		FrigateRecordingsStateTopic: "frigate/werkstatt/recordings/state",
		FrigateDetectStatusTopic:    "frigate/werkstatt/status/detect",
		FrigateCameraName:           "werkstatt",
		DoorTopic:                   "werkstatt/door",
		BridgeStateTopic:            "$SYS/broker/connection/shop.shop/state",
		ExitDelay:                   60 * time.Second,
		DoorEscalation:              60 * time.Second,
		EntryDelay:                  30 * time.Second,
		PreArmSweep:                 60 * time.Second,
		ReconcileDelay:              10 * time.Millisecond,
		VerifyInitialDelay:          5 * time.Millisecond,
		VerifyRetryDelay:            5 * time.Millisecond,
		MismatchNotifyInterval:      time.Hour,
		DiscoveryPrefix:             "homeassistant",
	}
}

func newTestEngine(t *testing.T) (*Engine, *fakeClient, *recordingNotifier, *metrics.Recorder) {
	t.Helper()
	cfg := testEngineConfig(t)
	cfg.StateFile = t.TempDir() + "/state.json" // never exists -> fresh bootstrap
	client := newFakeClient()
	notifier := &recordingNotifier{}
	rec := metrics.NewRecorder()
	core := NewCore(cfg, nil, nil)
	e := NewEngine(cfg, client, core, notifier, rec)
	core.SetSink(e)
	return e, client, notifier, rec
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("timeout waiting for: " + msg)
}

// ---- tests ----------------------------------------------------------------

func TestMQTTTopicMatches(t *testing.T) {
	cases := []struct {
		pattern, topic string
		want           bool
	}{
		{"tele/+/LWT", "tele/gosund-0/LWT", true},
		{"tele/+/LWT", "tele/gosund-0/STATE", false},
		{"werkstatt/#", "werkstatt/door", true},
		{"werkstatt/#", "werkstatt/alarm/state", true},
		{"werkstatt/door", "werkstatt/door", true},
		{"werkstatt/door", "werkstatt/door/x", false},
		{"+/alarm/#", "werkstatt/alarm/state", true},
	}
	for _, c := range cases {
		if got := mqttTopicMatches(c.pattern, c.topic); got != c.want {
			t.Errorf("mqttTopicMatches(%q, %q) = %v, want %v", c.pattern, c.topic, got, c.want)
		}
	}
}

func TestDeviceFromLWT(t *testing.T) {
	cases := []struct{ topic, want string }{
		{"tele/gosund-0/LWT", "gosund-0"},
		{"werkstatt/gosund-0/tele/LWT", "gosund-0"},
		{"gosund-0/LWT", "gosund-0"},
		{"tele/sonoff-2/LWT", "sonoff-2"},
	}
	for _, c := range cases {
		if got := deviceFromLWT(c.topic); got != c.want {
			t.Errorf("deviceFromLWT(%q) = %q, want %q", c.topic, got, c.want)
		}
	}
}

func TestStartPublishesAvailabilityDiscoveryAndState(t *testing.T) {
	e, client, _, _ := newTestEngine(t)
	e.Start()

	// Fresh boot, no armed evidence: bootstrap -> disarmed. Waiting for the
	// profile publish synchronizes on the *completed* reconcile callback
	// (the second SaveState runs before it and would otherwise race the
	// test's TempDir cleanup).
	waitFor(t, time.Second, func() bool { return client.pubCount("frigate/profile/set") > 0 }, "bootstrap")

	avail := client.pubsOn("werkstatt/alarm/available")
	if len(avail) == 0 || avail[0].payload != "online" || !avail[0].retained {
		t.Fatalf("availability not published retained online: %+v", avail)
	}
	disc := client.pubsOn("homeassistant/alarm_control_panel/werkstatt_alarm/config")
	if len(disc) == 0 || !disc[0].retained {
		t.Fatal("discovery not published retained")
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(disc[0].payload), &d); err != nil {
		t.Fatalf("discovery payload not JSON: %v", err)
	}
	if d["state_topic"] != "werkstatt/alarm/state" || d["command_topic"] != "werkstatt/alarm/cmnd" {
		t.Fatalf("discovery topics wrong: %v", d)
	}
	if d["json_attributes_topic"] != "werkstatt/alarm/attributes" {
		t.Fatalf("discovery attributes topic missing: %v", d)
	}
	// A separate button gives ACK a UI verb (HA's alarm panel has none).
	btn := client.pubsOn("homeassistant/button/werkstatt_alarm_ack/config")
	if len(btn) == 0 || !btn[0].retained {
		t.Fatal("ACK button discovery not published retained")
	}
	var b map[string]any
	if err := json.Unmarshal([]byte(btn[0].payload), &b); err != nil {
		t.Fatalf("button discovery payload not JSON: %v", err)
	}
	if b["command_topic"] != "werkstatt/alarm/cmnd" || b["payload_press"] != "ACK" {
		t.Fatalf("button discovery wrong: %v", b)
	}
	if got := client.pubsOn("werkstatt/alarm/state")[0].payload; got != "disarmed" {
		t.Fatalf("fresh boot state = %q, want disarmed", got)
	}
}

// The retained-state storm at connect must produce exactly one profile/set
// publish; repeated reconciliation triggers while a verification is in
// flight are debounced (smoke-test finding 1, fixed).
func TestConnectStormDebouncesProfileSet(t *testing.T) {
	e, client, _, _ := newTestEngine(t)
	e.cfg.VerifyInitialDelay = 200 * time.Millisecond // keep the verify in flight
	e.Start()

	// Retained storm: armed evidence everywhere.
	client.deliver("frigate/available", "online")
	client.deliver("frigate/werkstatt/detect/state", "ON")
	client.deliver("frigate/werkstatt/recordings/state", "ON")
	client.deliver("frigate/profile/state", "armed")
	client.deliver("frigate/werkstatt/status/detect", "online")
	client.deliver("werkstatt/door", "closed")
	client.deliver("$SYS/broker/connection/shop.shop/state", "1")

	// Wait for the full reconcile callback (profile/set is its last publish;
	// observing the earlier state publish alone races the second SaveState).
	waitFor(t, time.Second, func() bool {
		return client.pubCount("frigate/profile/set") > 0
	}, "bootstrap profile publish")

	if got := client.pubsOn("werkstatt/alarm/state")[0].payload; got != "armed_away" {
		t.Fatalf("boot state = %q, want armed_away (evidence)", got)
	}
	if n := client.pubCount("frigate/profile/set"); n != 1 {
		t.Fatalf("profile/set published %d times during connect storm, want 1", n)
	}
	if got := client.pubsOn("frigate/profile/set")[0].payload; got != "armed" {
		t.Fatalf("profile = %q, want armed", got)
	}

	// Repeated reconciliation while the verification is pending is debounced.
	e.VerifyProfile()
	client.deliver("frigate/available", "online")
	if n := client.pubCount("frigate/profile/set"); n != 1 {
		t.Fatalf("profile/set published %d times after re-reconcile, want 1", n)
	}
}

// A profile mismatch faults and notifies once, not on every verify cycle
// (smoke-test finding 2, fixed: fault both directions, notify once).
func TestProfileMismatchNotifiesOnce(t *testing.T) {
	e, client, notifier, _ := newTestEngine(t)
	e.Start()

	// Disarmed boot, clean switch states (detect/recordings OFF).
	client.deliver("frigate/available", "online")
	client.deliver("frigate/werkstatt/detect/state", "OFF")
	client.deliver("frigate/werkstatt/recordings/state", "OFF")
	client.deliver("frigate/profile/state", "disarmed")
	client.deliver("werkstatt/door", "closed")
	// The clean verification completes (its success publishes the fault topic).
	waitFor(t, time.Second, func() bool { return client.pubCount("werkstatt/alarm/fault") > 0 }, "clean verify")

	// Privacy regression: recordings turn ON while disarmed, with a
	// verification in flight so the mismatch is detected.
	e.VerifyProfile()
	client.deliver("frigate/werkstatt/recordings/state", "ON")
	waitFor(t, time.Second, func() bool {
		return notifier.count("Werkstatt Frigate profile mismatch") == 1
	}, "first mismatch notification")

	// Further verify cycles must not repeat the notification.
	e.VerifyProfile()
	e.verifyStep()
	if n := notifier.count("Werkstatt Frigate profile mismatch"); n != 1 {
		t.Fatalf("mismatch notified %d times, want 1", n)
	}
	if !e.core.HasFault("profile_mismatch") {
		t.Fatal("profile_mismatch fault not set")
	}
}

// Arming through the MQTT command path exercises the full engine wiring:
// arming -> door closed -> armed_away.
func TestArmCommandPublishesArmedAway(t *testing.T) {
	e, client, _, _ := newTestEngine(t)
	e.Start()
	client.deliver("frigate/available", "online")
	client.deliver("frigate/werkstatt/detect/state", "OFF")
	client.deliver("frigate/werkstatt/recordings/state", "OFF")
	client.deliver("werkstatt/door", "closed")
	client.deliver("$SYS/broker/connection/shop.shop/state", "1")
	waitFor(t, time.Second, func() bool { return client.pubCount("frigate/profile/set") > 0 }, "bootstrap")

	client.deliver("werkstatt/alarm/cmnd", "ARM_AWAY")
	waitFor(t, time.Second, func() bool {
		return len(client.pubsOn("werkstatt/alarm/state")) > 0 &&
			client.pubsOn("werkstatt/alarm/state")[len(client.pubsOn("werkstatt/alarm/state"))-1].payload == "arming"
	}, "arming publish")

	client.deliver("werkstatt/door", "closed") // re-close -> armed immediately
	waitFor(t, time.Second, func() bool {
		pubs := client.pubsOn("werkstatt/alarm/state")
		return pubs[len(pubs)-1].payload == "armed_away"
	}, "armed_away publish")
}

// Device LWT messages feed the metrics recorder (exporter fold).
func TestDeviceLWTUpdatesMetrics(t *testing.T) {
	cfg := testEngineConfig(t)
	cfg.StateFile = t.TempDir() + "/state.json"
	cfg.DeviceLWTPatterns = []string{"tele/+/LWT"}
	client := newFakeClient()
	rec := metrics.NewRecorder()
	core := NewCore(cfg, nil, nil)
	e := NewEngine(cfg, client, core, &recordingNotifier{}, rec)
	core.SetSink(e)
	e.Start()

	client.deliver("tele/gosund-0/LWT", "Online")
	client.deliver("tele/sonoff-2/LWT", "Offline")

	out := rec.Render()
	if !strings.Contains(out, `werkstatt_device_up{device="gosund-0"} 1`) {
		t.Errorf("missing gosund-0 gauge:\n%s", out)
	}
	if !strings.Contains(out, `werkstatt_device_up{device="sonoff-2"} 0`) {
		t.Errorf("missing sonoff-2 gauge:\n%s", out)
	}
}

// Metrics reflect the MQTT-derived state (exporter fold).
func TestMetricsReflectAlarmState(t *testing.T) {
	e, client, _, rec := newTestEngine(t)
	e.Start()
	client.deliver("frigate/available", "online")
	client.deliver("frigate/werkstatt/detect/state", "ON")
	client.deliver("frigate/werkstatt/recordings/state", "ON")
	client.deliver("frigate/profile/state", "armed")
	client.deliver("werkstatt/door", "closed")
	client.deliver("$SYS/broker/connection/shop.shop/state", "1")
	// Wait until the full reconcile ran (profile/set is published after the
	// state's SaveState, so the gauges and the disk state are settled).
	waitFor(t, time.Second, func() bool {
		return client.pubCount("frigate/profile/set") > 0 &&
			strings.Contains(rec.Render(), `werkstatt_alarm_state{state="armed_away"} 1`)
	}, "bootstrap reconcile")

	out := rec.Render()
	for _, want := range []string{
		"werkstatt_bridge_connected 1",
		"werkstatt_frigate_available 1",
		"werkstatt_detect_enabled 1",
		"werkstatt_recordings_enabled 1",
		"werkstatt_door_open 0",
		"werkstatt_door_known 1",
		`werkstatt_profile_active{profile="armed"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics missing %q", want)
		}
	}
}

// A persisted state file at startup must not panic: NewEngine restores it
// before main.go installs the engine as sink, and reconcile() replays the
// publishes on connect (crash loop observed live 2026-09-21, exit code 2).
func TestNewEngineWithExistingStateFile(t *testing.T) {
	cfg := testEngineConfig(t)
	cfg.StateFile = t.TempDir() + "/state.json"
	if err := SaveState(cfg.StateFile, Data{State: StateArmedAway, Updated: time.Now()}); err != nil {
		t.Fatal(err)
	}

	client := newFakeClient()
	rec := metrics.NewRecorder()
	core := NewCore(cfg, nil, nil)
	e := NewEngine(cfg, client, core, &recordingNotifier{}, rec) // must not panic
	core.SetSink(e)
	e.Start()

	// The restored state is re-established on connect (reconcile replay).
	waitFor(t, time.Second, func() bool { return client.pubCount("frigate/profile/set") > 0 }, "reconcile")
	subs := client.pubsOn("werkstatt/alarm/state")
	if len(subs) == 0 || subs[len(subs)-1].payload != "armed_away" {
		t.Fatalf("state not re-published after restore: %+v", subs)
	}
}

// A real Frigate 0.18 review payload nests the alerting objects under
// after.data; the engine must read that path. Regression for the
// 2026-09-24 incident, where parsing after.objects yielded an empty list and
// person reviews never entered the pending/entry-delay path (and review_id
// was never published in the attributes).
func TestPersonReviewFromFrigatePayloadTriggers(t *testing.T) {
	e, client, _, _ := newTestEngine(t)
	e.Start()
	client.deliver("frigate/available", "online")
	client.deliver("frigate/werkstatt/detect/state", "OFF")
	client.deliver("frigate/werkstatt/recordings/state", "OFF")
	client.deliver("werkstatt/door", "closed")
	client.deliver("$SYS/broker/connection/shop.shop/state", "1")
	waitFor(t, time.Second, func() bool { return client.pubCount("frigate/profile/set") > 0 }, "bootstrap")

	client.deliver("werkstatt/alarm/cmnd", "ARM_AWAY")
	client.deliver("werkstatt/door", "closed") // close during the exit delay -> armed_away
	waitFor(t, time.Second, func() bool {
		pubs := client.pubsOn("werkstatt/alarm/state")
		return len(pubs) > 0 && pubs[len(pubs)-1].payload == "armed_away"
	}, "armed_away")

	// Exactly the shape Frigate publishes on frigate/reviews (see the 0.18
	// MQTT docs): the object list lives under after.data.
	review := `{"type":"new","before":{},"after":{"id":"1727.1-abc","camera":"werkstatt",` +
		`"start_time":1727.1,"end_time":null,"severity":"alert","thumb_path":"/x",` +
		`"data":{"detections":["1727.1-abc"],"objects":["person"],"sub_labels":[],"zones":[],"audio":[]}}}`
	client.deliver("frigate/reviews", review)

	waitFor(t, time.Second, func() bool {
		pubs := client.pubsOn("werkstatt/alarm/state")
		return len(pubs) > 0 && pubs[len(pubs)-1].payload == "pending"
	}, "pending after person review")

	// The review id reaches the attributes (it did not before the fix).
	var attrs struct {
		ReviewID string `json:"review_id"`
	}
	attrPubs := client.pubsOn("werkstatt/alarm/attributes")
	if len(attrPubs) == 0 {
		t.Fatal("no attributes published")
	}
	if err := json.Unmarshal([]byte(attrPubs[len(attrPubs)-1].payload), &attrs); err != nil {
		t.Fatalf("attributes not JSON: %v", err)
	}
	if attrs.ReviewID != "1727.1-abc" {
		t.Fatalf("review_id = %q, want 1727.1-abc", attrs.ReviewID)
	}
}
