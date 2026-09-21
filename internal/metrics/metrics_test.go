package metrics

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func fp(v float64) *float64 { return &v }

func TestRenderKnownValues(t *testing.T) {
	r := NewRecorder()
	r.SetAlarmState("armed_away")
	r.SetDoor(true)
	r.SetBridge(true)
	r.SetFrigateAvailable(true)
	r.SetCameraOnline(false)
	r.SetDetect(true)
	r.SetRecordings(false)
	r.SetProfile("armed")
	r.SetDevice("gosund-0", true)
	r.SetDevice("sonoff-2", false)
	r.SetStats(Stats{
		CameraFPS:         fp(6.2),
		DetectionFPS:      fp(13.7),
		ConnectionQuality: fp(97.5),
		Reconnects:        fp(2),
		Stalls:            fp(1),
	})
	r.SetShopReachable(true)

	out := r.Render()
	for _, want := range []string{
		`werkstatt_alarm_state{state="armed_away"} 1`,
		"werkstatt_door_known 1",
		"werkstatt_door_open 1",
		"werkstatt_bridge_connected 1",
		"werkstatt_frigate_available 1",
		"werkstatt_camera_online 0",
		"werkstatt_detect_enabled 1",
		"werkstatt_recordings_enabled 0",
		`werkstatt_profile_active{profile="armed"} 1`,
		`werkstatt_device_up{device="gosund-0"} 1`,
		`werkstatt_device_up{device="sonoff-2"} 0`,
		"werkstatt_camera_fps 6.2",
		"werkstatt_detection_fps 13.7",
		"werkstatt_connection_quality 97.5",
		"werkstatt_reconnects_total 2",
		"werkstatt_stalls_total 1",
		"werkstatt_shop_reachable 1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestRenderUnknownOmitted(t *testing.T) {
	r := NewRecorder()
	out := r.Render()

	if !strings.Contains(out, "werkstatt_door_known 0") {
		t.Error("door_known must be exported as 0 before any door message")
	}
	for _, absent := range []string{
		"werkstatt_door_open",
		"werkstatt_bridge_connected",
		"werkstatt_frigate_available",
		"werkstatt_camera_online",
		"werkstatt_detect_enabled",
		"werkstatt_recordings_enabled",
		"werkstatt_profile_active",
		"werkstatt_device_up",
		"werkstatt_camera_fps",
		"werkstatt_shop_reachable",
	} {
		if strings.Contains(out, absent) {
			t.Errorf("unknown metric %q must be omitted, got:\n%s", absent, out)
		}
	}
}

func TestNilRecorderIsSafe(t *testing.T) {
	var r *Recorder
	r.SetAlarmState("disarmed")
	r.SetDoor(false)
	r.SetDevice("x", true)
	if r.Render() != "" {
		t.Error("nil recorder must render empty")
	}
}

func TestProber(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	r := NewRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.StartProber(ctx, ln.Addr().String(), 5*time.Millisecond)

	waitFor(t, func() bool { return strings.Contains(r.Render(), "werkstatt_shop_reachable 1") }, "reachable")

	_ = ln.Close()
	waitFor(t, func() bool { return strings.Contains(r.Render(), "werkstatt_shop_reachable 0") }, "unreachable")
}

func TestStatsPoller(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/stats" {
			http.NotFound(w, req)
			return
		}
		fmt.Fprint(w, `{"detection_fps":13.7,"cameras":{"werkstatt":{"camera_fps":6.2,"connection_quality":97.5,"reconnects":2,"stalls":1},"other":{"camera_fps":1.0}}}`)
	}))
	defer srv.Close()

	r := NewRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.StartStatsPoller(ctx, StatsOptions{BaseURL: srv.URL, Camera: "werkstatt"}, 5*time.Millisecond)

	waitFor(t, func() bool { return strings.Contains(r.Render(), "werkstatt_detection_fps 13.7") }, "stats")
	out := r.Render()
	for _, want := range []string{"werkstatt_camera_fps 6.2", "werkstatt_connection_quality 97.5"} {
		if !strings.Contains(out, want) {
			t.Errorf("stats output missing %q:\n%s", want, out)
		}
	}
}

func TestStatsPollerOptionalFieldsOmitted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"detection_fps":5.0,"cameras":{"werkstatt":{"camera_fps":3.0}}}`)
	}))
	defer srv.Close()

	r := NewRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.StartStatsPoller(ctx, StatsOptions{BaseURL: srv.URL, Camera: "werkstatt"}, 5*time.Millisecond)

	waitFor(t, func() bool { return strings.Contains(r.Render(), "werkstatt_camera_fps 3") }, "stats")
	out := r.Render()
	for _, absent := range []string{"werkstatt_connection_quality", "werkstatt_reconnects_total", "werkstatt_stalls_total"} {
		if strings.Contains(out, absent) {
			t.Errorf("absent stats field %q exported:\n%s", absent, out)
		}
	}
}

func TestStatsPollerAuth(t *testing.T) {
	var (
		mu      sync.Mutex
		gotUser string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		u, _, _ := req.BasicAuth()
		mu.Lock()
		gotUser = u
		mu.Unlock()
		fmt.Fprint(w, `{"detection_fps":1}`)
	}))
	defer srv.Close()

	r := NewRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.StartStatsPoller(ctx, StatsOptions{BaseURL: srv.URL, User: "frigate", Pass: "secret", Camera: "werkstatt"}, 5*time.Millisecond)

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return gotUser == "frigate"
	}, "basic auth header")
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("timeout waiting for: " + msg)
}
