// Package metrics implements the werkstatt-* Prometheus gauges for the
// monitoring dashboard (see monitoring/README.md). The exporter is folded
// into shop-alarm instead of running as a standalone service: shop-alarm
// already subscribes to every MQTT topic behind these gauges.
//
// All Recorder methods are safe for concurrent use and nil-safe, so callers
// can use a zero *Recorder in tests.
package metrics

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Stats holds the Frigate /api/stats fields that are exported. Pointer
// fields distinguish "absent" from "zero"; the exact field names must be
// verified against the deployed Frigate version (see monitoring/README.md).
type Stats struct {
	CameraFPS         *float64
	DetectionFPS      *float64
	ConnectionQuality *float64
	Reconnects        *float64
	Stalls            *float64
}

// Recorder collects gauge values and renders them in the Prometheus text
// exposition format. Unknown values (never seen) are omitted from the
// output instead of being reported as zero.
type Recorder struct {
	mu sync.Mutex

	alarmState    string
	alarmStateSet bool

	doorKnown bool
	doorOpen  bool

	bridge           *bool
	frigateAvailable *bool
	cameraOnline     *bool
	detect           *bool
	recordings       *bool

	profile    string
	profileSet bool

	devices map[string]bool

	stats Stats

	frigateAPIDown *bool
	onFrigateAPI   func(down bool)
}

// NewRecorder returns an empty recorder.
func NewRecorder() *Recorder {
	return &Recorder{devices: make(map[string]bool)}
}

func b(v bool) float64 {
	if v {
		return 1
	}
	return 0
}

// SetFrigateAPIDown records whether the Frigate API polls are currently
// failing and invokes the listener on every change.
func (r *Recorder) SetFrigateAPIDown(down bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	old := r.frigateAPIDown != nil && *r.frigateAPIDown
	changed := r.frigateAPIDown == nil || old != down
	r.frigateAPIDown = &down
	fn := r.onFrigateAPI
	r.mu.Unlock()
	if changed && fn != nil {
		fn(down)
	}
}

// SetFrigateAPIListener installs the callback fired on Frigate API
// reachability transitions. Called from the stats poller goroutine.
func (r *Recorder) SetFrigateAPIListener(fn func(down bool)) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.onFrigateAPI = fn
	r.mu.Unlock()
}

// SetAlarmState records the active alarm state (disarmed, arming,
// armed_away, pending, triggered).
func (r *Recorder) SetAlarmState(state string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.alarmState, r.alarmStateSet = state, true
}

// SetDoor records the door reed state.
func (r *Recorder) SetDoor(open bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.doorKnown, r.doorOpen = true, open
}

// SetBridge records the shop bridge connection state as seen on opus.
func (r *Recorder) SetBridge(up bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bridge = &up
}

// SetFrigateAvailable records frigate/available (online => true).
func (r *Recorder) SetFrigateAvailable(online bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frigateAvailable = &online
}

// SetCameraOnline records frigate/<camera>/status/detect (stream health).
func (r *Recorder) SetCameraOnline(online bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cameraOnline = &online
}

// SetDetect records the authoritative detect switch state.
func (r *Recorder) SetDetect(on bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.detect = &on
}

// SetRecordings records the authoritative recordings switch state.
func (r *Recorder) SetRecordings(on bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordings = &on
}

// SetProfile records frigate/profile/state.
func (r *Recorder) SetProfile(profile string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.profile, r.profileSet = profile, true
}

// SetDevice records a device LWT (tele/<device>/LWT Online/Offline).
func (r *Recorder) SetDevice(device string, up bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.devices[device] = up
}

// SetStats records the Frigate /api/stats snapshot.
func (r *Recorder) SetStats(s Stats) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stats = s
}

func gauge(w *strings.Builder, name, help string, value float64) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n%s %s\n", name, help, name, name, formatFloat(value))
}

func gaugeLabel(w *strings.Builder, name, help, label, value string, v float64) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n%s{%s=%q} %s\n",
		name, help, name, name, label, escapeLabel(value), formatFloat(v))
}

func ptrGauge(w *strings.Builder, name, help string, v *float64) {
	if v == nil {
		return
	}
	gauge(w, name, help, *v)
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

func escapeLabel(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return strings.ReplaceAll(s, "\n", `\n`)
}

// Render returns the current gauges in the Prometheus text format.
func (r *Recorder) Render() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	var w strings.Builder

	if r.alarmStateSet {
		gaugeLabel(&w, "werkstatt_alarm_state", "Alarm state (1 = active state).", "state", r.alarmState, 1)
	}

	gauge(&w, "werkstatt_door_known", "Door reed state known (1) or unknown (0).", b(r.doorKnown))
	if r.doorKnown {
		gauge(&w, "werkstatt_door_open", "Workshop door open (1) or closed (0).", b(r.doorOpen))
	}

	if r.bridge != nil {
		gauge(&w, "werkstatt_bridge_connected", "MQTT bridge shop->opus connected as observed on opus.", b(*r.bridge))
	}
	if r.frigateAvailable != nil {
		gauge(&w, "werkstatt_frigate_available", "Frigate process available (frigate/available).", b(*r.frigateAvailable))
	}
	if r.cameraOnline != nil {
		gauge(&w, "werkstatt_camera_online", "End-to-end camera stream healthy (status/detect).", b(*r.cameraOnline))
	}
	if r.detect != nil {
		gauge(&w, "werkstatt_detect_enabled", "Frigate detection switch state.", b(*r.detect))
	}
	if r.recordings != nil {
		gauge(&w, "werkstatt_recordings_enabled", "Frigate recordings switch state.", b(*r.recordings))
	}

	if r.profileSet {
		gaugeLabel(&w, "werkstatt_profile_active", "Active Frigate profile.", "profile", r.profile, 1)
	}

	if len(r.devices) > 0 {
		devices := make([]string, 0, len(r.devices))
		for d := range r.devices {
			devices = append(devices, d)
		}
		sort.Strings(devices)
		for _, d := range devices {
			gaugeLabel(&w, "werkstatt_device_up", "Device LWT online (1) or offline (0).", "device", d, b(r.devices[d]))
		}
	}

	ptrGauge(&w, "werkstatt_camera_fps", "Camera decode fps (Frigate /api/stats).", r.stats.CameraFPS)
	ptrGauge(&w, "werkstatt_detection_fps", "Global detection fps (Frigate /api/stats).", r.stats.DetectionFPS)
	ptrGauge(&w, "werkstatt_connection_quality", "Camera connection quality percentile (Frigate /api/stats).", r.stats.ConnectionQuality)
	ptrGauge(&w, "werkstatt_reconnects_total", "Camera reconnect counter (Frigate /api/stats).", r.stats.Reconnects)
	ptrGauge(&w, "werkstatt_stalls_total", "Camera stall counter (Frigate /api/stats).", r.stats.Stalls)

	if r.frigateAPIDown != nil {
		gauge(&w, "werkstatt_frigate_api_reachable", "Frigate API reachable (0 = last polls failed).", b(!*r.frigateAPIDown))
	}

	return w.String()
}
