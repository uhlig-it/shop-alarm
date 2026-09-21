package metrics

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
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
	BaseURL       string // e.g. http://opus:5000 (no trailing slash)
	User          string // basic auth (optional)
	Pass          string
	Camera        string // camera whose per-camera fields are exported
	FailThreshold int    // consecutive poll failures before the API counts as down
}

// StartStatsPoller polls Frigate /api/stats until ctx is cancelled and
// records the configured camera's fields. It returns immediately.
func (r *Recorder) StartStatsPoller(ctx context.Context, opts StatsOptions, interval time.Duration) {
	if r == nil || opts.BaseURL == "" || interval <= 0 {
		return
	}
	if opts.FailThreshold <= 0 {
		opts.FailThreshold = 1
	}
	go func() {
		client := &http.Client{Timeout: 10 * time.Second}
		failures := 0
		poll := func() {
			// Only transport/HTTP errors count as an unreachable API; a body
			// we cannot parse (e.g. an unexpected stats shape from a
			// different camera) is a warning, never an API-down signal.
			body, err := fetchStats(client, opts)
			if err != nil {
				slog.Warn("frigate stats poll failed", "error", err)
				failures++
				if failures >= opts.FailThreshold {
					r.SetFrigateAPIDown(true)
				}
				return
			}
			failures = 0
			r.SetFrigateAPIDown(false)
			stats, perr := parseStats(body, opts.Camera)
			if perr != nil {
				slog.Warn("frigate stats poll unparsable", "error", perr)
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

func fetchStats(client *http.Client, opts StatsOptions) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, opts.BaseURL+"/api/stats", nil)
	if err != nil {
		return nil, err
	}
	if opts.User != "" {
		req.SetBasicAuth(opts.User, opts.Pass)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, &statsError{status: resp.Status}
	}
	return body, nil
}

type statsError struct{ status string }

func (e *statsError) Error() string { return "frigate stats returned " + e.status }

// num coerces a JSON number or numeric string to a float. Anything else
// (e.g. "N/A" for a dead camera's quality) yields nil, so one broken
// camera never kills the rest of the stats.
func num(raw json.RawMessage) *float64 {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return nil
	}
	s = strings.Trim(s, `"`)
	if s == "" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

// parseStats extracts the exported fields. Optional fields that the
// deployed Frigate version does not publish, or reports as strings, stay
// nil and are omitted from the metrics output.
func parseStats(body []byte, camera string) (Stats, error) {
	var doc struct {
		DetectionFPS json.RawMessage `json:"detection_fps"`
		Cameras      map[string]struct {
			CameraFPS         json.RawMessage `json:"camera_fps"`
			ConnectionQuality json.RawMessage `json:"connection_quality"`
			Reconnects        json.RawMessage `json:"reconnects"`
			Stalls            json.RawMessage `json:"stalls"`
		} `json:"cameras"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return Stats{}, err
	}
	s := Stats{DetectionFPS: num(doc.DetectionFPS)}
	if cam, ok := doc.Cameras[camera]; ok {
		s.CameraFPS = num(cam.CameraFPS)
		s.ConnectionQuality = num(cam.ConnectionQuality)
		s.Reconnects = num(cam.Reconnects)
		s.Stalls = num(cam.Stalls)
	}
	return s, nil
}
