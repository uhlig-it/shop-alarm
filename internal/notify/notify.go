// Package notify implements the ntfy backup notification channel for
// alarm-core. It posts JSON messages to a configured ntfy URL and, when a
// Frigate API is configured and a review id is known, attaches the review
// snapshot as a multipart file. All failures degrade to a log line so the
// alarm state machine never depends on notifications.
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"time"
)

// Notifier posts to ntfy and optionally fetches Frigate review snapshots.
type Notifier struct {
	url        string
	liveStream string
	client     *http.Client

	frigateAPIURL string
	frigateUser   string
	frigatePass   string
}

// Config describes the notification options (subset of config.Config).
type Config struct {
	NTFYURL        string
	LiveStreamURL  string
	FrigateAPIURL  string
	FrigateAPIUser string
	FrigateAPIPass string
	Timeout        time.Duration
}

// New returns a Notifier. An empty NTFYURL disables delivery.
func New(cfg Config) *Notifier {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	return &Notifier{
		url:           cfg.NTFYURL,
		liveStream:    cfg.LiveStreamURL,
		client:        &http.Client{Timeout: timeout},
		frigateAPIURL: cfg.FrigateAPIURL,
		frigateUser:   cfg.FrigateAPIUser,
		frigatePass:   cfg.FrigateAPIPass,
	}
}

// Enabled reports whether the backup channel is configured.
func (n *Notifier) Enabled() bool { return n.url != "" }

// Notify sends a notification. priority follows ntfy's scale 1..5
// (default 3). reviewID optionally attaches the Frigate review snapshot.
func (n *Notifier) Notify(priority int, title, message, reviewID string) {
	if !n.Enabled() {
		slog.Info("notification skipped (ntfy not configured)", "title", title)
		return
	}
	if priority < 1 || priority > 5 {
		priority = 3
	}
	if n.liveStream != "" {
		message += "\n\nLive: " + n.liveStream
	}

	payload := map[string]any{
		"message":  message,
		"priority": priority,
	}
	if title != "" {
		payload["title"] = title
	}

	var (
		req *http.Request
		err error
	)
	snapshot, snapshotErr := n.fetchSnapshot(reviewID)
	if snapshot != nil {
		req, err = n.multipartRequest(payload, title, snapshot)
		_ = snapshot.Close()
	} else {
		if snapshotErr != nil {
			slog.Warn("snapshot unavailable, sending text-only", "error", snapshotErr)
		}
		b, _ := json.Marshal(payload)
		req, err = http.NewRequest(http.MethodPost, n.url, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
	}

	if err != nil {
		slog.Error("notification failed to build", "error", err)
		return
	}
	resp, err := n.client.Do(req)
	if err != nil {
		slog.Error("notification failed", "title", title, "error", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode >= 300 {
		slog.Error("notification rejected", "status", resp.Status, "body", string(body))
		return
	}
	slog.Info("notification sent", "title", title, "priority", priority)
}

// fetchSnapshot downloads the Frigate review snapshot for the review id.
// Returning (nil, nil) means: no snapshot configured.
func (n *Notifier) fetchSnapshot(reviewID string) (io.ReadCloser, error) {
	if n.frigateAPIURL == "" || reviewID == "" {
		return nil, nil
	}
	u, err := url.Parse(n.frigateAPIURL)
	if err != nil {
		return nil, err
	}
	u.Path = u.Path + "/api/review/" + url.PathEscape(reviewID) + "/snapshot.jpg"
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if n.frigateUser != "" {
		req.SetBasicAuth(n.frigateUser, n.frigatePass)
	}
	resp, err := n.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("frigate snapshot returned %s", resp.Status)
	}
	if resp.ContentLength == 0 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("frigate snapshot empty")
	}
	return resp.Body, nil
}

func (n *Notifier) multipartRequest(payload map[string]any, title string, file io.Reader) (*http.Request, error) {
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	msg, _ := payload["message"].(string)
	if err := w.WriteField("message", msg); err != nil {
		return nil, err
	}
	if title != "" {
		if err := w.WriteField("title", title); err != nil {
			return nil, err
		}
	}
	if p, ok := payload["priority"].(int); ok {
		if err := w.WriteField("priority", fmt.Sprintf("%d", p)); err != nil {
			return nil, err
		}
	}
	fw, err := w.CreateFormFile("file", "snapshot.jpg")
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(fw, file); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, n.url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req, nil
}
