package metrics

import (
	"context"
	"fmt"
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

// Frigate 0.18 reports some per-camera fields as strings (observed:
// terrasse.connection_quality); a broken/never-connected camera must not
// kill the whole stats parse, and the busy camera's numbers must survive.
func TestParseStatsToleratesStringFields(t *testing.T) {
	body := `{"detection_fps":13.7,"cameras":{"werkstatt":{"camera_fps":6.2,"connection_quality":"97.5"},"terrasse":{"camera_fps":"N/A","connection_quality":"N/A"}}}`
	s, err := parseStats([]byte(body), "werkstatt")
	if err != nil {
		t.Fatalf("parse with string fields failed: %v", err)
	}
	if s.DetectionFPS == nil || *s.DetectionFPS != 13.7 {
		t.Fatalf("detection_fps = %v", s.DetectionFPS)
	}
	if s.CameraFPS == nil || *s.CameraFPS != 6.2 {
		t.Fatalf("camera_fps = %v", s.CameraFPS)
	}
	if s.ConnectionQuality == nil || *s.ConnectionQuality != 97.5 {
		t.Fatalf("connection_quality = %v", s.ConnectionQuality)
	}

	s2, err := parseStats([]byte(body), "terrasse")
	if err != nil {
		t.Fatalf("parse for the broken camera failed: %v", err)
	}
	if s2.CameraFPS != nil || s2.ConnectionQuality != nil {
		t.Fatalf("N/A string fields must become nil, got %+v", s2)
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

// Poll failures must flip the API-down signal only after the threshold, and
// a successful poll must flip it back (camera-loss rule input).
func TestStatsPollerFailuresFlipAPIDown(t *testing.T) {
	var (
		mu sync.Mutex
		n  int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		n++
		count := n
		mu.Unlock()
		if count <= 2 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, `{"detection_fps":1}`)
	}))
	defer srv.Close()

	r := NewRecorder()
	transitions := make(chan bool, 4)
	r.SetFrigateAPIListener(func(d bool) {
		transitions <- d
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Threshold 2: two failed polls -> down; the next success -> up.
	r.StartStatsPoller(ctx, StatsOptions{BaseURL: srv.URL, Camera: "werkstatt", FailThreshold: 2}, 5*time.Millisecond)

	select {
	case v := <-transitions:
		if !v {
			t.Fatalf("first transition = %v, want down=true after threshold", v)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no down transition")
	}
	select {
	case v := <-transitions:
		if v {
			t.Fatalf("second transition = %v, want down=false on recovery", v)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no recovery transition")
	}
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
