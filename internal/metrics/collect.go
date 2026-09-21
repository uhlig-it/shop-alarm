package metrics

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// probeTimeout bounds a single TCP reachability probe.
const probeTimeout = 3 * time.Second

// StartProber probes the shop broker over TCP until ctx is cancelled and
// records the result as werkstatt_shop_reachable. It returns immediately.
func (r *Recorder) StartProber(ctx context.Context, addr string, interval time.Duration) {
	if r == nil || addr == "" || interval <= 0 {
		return
	}
	go func() {
		probe := func() {
			conn, err := net.DialTimeout("tcp", addr, probeTimeout)
			if err == nil {
				_ = conn.Close()
			}
			r.SetShopReachable(err == nil)
			if err != nil {
				slog.Debug("shop probe failed", "addr", addr, "error", err)
			}
		}
		probe()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				probe()
			}
		}
	}()
}

// StatsOptions describes the Frigate stats endpoint.
type StatsOptions struct {
	BaseURL string // e.g. http://opus:5000 (no trailing slash)
	User    string // basic auth (optional)
	Pass    string
	Camera  string // camera whose per-camera fields are exported
}

// StartStatsPoller polls Frigate /api/stats until ctx is cancelled and
// records the configured camera's fields. It returns immediately.
func (r *Recorder) StartStatsPoller(ctx context.Context, opts StatsOptions, interval time.Duration) {
	if r == nil || opts.BaseURL == "" || interval <= 0 {
		return
	}
	go func() {
		client := &http.Client{Timeout: 10 * time.Second}
		poll := func() {
			stats, err := fetchStats(client, opts)
			if err != nil {
				slog.Warn("frigate stats poll failed", "error", err)
				return
			}
			r.SetStats(stats)
		}
		poll()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				poll()
			}
		}
	}()
}

func fetchStats(client *http.Client, opts StatsOptions) (Stats, error) {
	req, err := http.NewRequest(http.MethodGet, opts.BaseURL+"/api/stats", nil)
	if err != nil {
		return Stats{}, err
	}
	if opts.User != "" {
		req.SetBasicAuth(opts.User, opts.Pass)
	}
	resp, err := client.Do(req)
	if err != nil {
		return Stats{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Stats{}, err
	}
	if resp.StatusCode >= 300 {
		return Stats{}, &statsError{status: resp.Status}
	}
	return parseStats(body, opts.Camera)
}

type statsError struct{ status string }

func (e *statsError) Error() string { return "frigate stats returned " + e.status }

// parseStats extracts the exported fields. Optional fields that the
// deployed Frigate version does not publish stay nil and are omitted from
// the metrics output.
func parseStats(body []byte, camera string) (Stats, error) {
	var doc struct {
		DetectionFPS *float64 `json:"detection_fps"`
		Cameras      map[string]struct {
			CameraFPS         *float64 `json:"camera_fps"`
			ConnectionQuality *float64 `json:"connection_quality"`
			Reconnects        *float64 `json:"reconnects"`
			Stalls            *float64 `json:"stalls"`
		} `json:"cameras"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return Stats{}, err
	}
	s := Stats{DetectionFPS: doc.DetectionFPS}
	if cam, ok := doc.Cameras[camera]; ok {
		s.CameraFPS = cam.CameraFPS
		s.ConnectionQuality = cam.ConnectionQuality
		s.Reconnects = cam.Reconnects
		s.Stalls = cam.Stalls
	}
	return s, nil
}
