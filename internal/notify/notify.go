// Package notify implements the ntfy backup notification channel for
// shop-alarm. It posts messages to a configured ntfy URL and, when a Frigate
// API is configured and a review id is known, attaches the review snapshot as
// a local-file upload. All failures degrade to a log line so the alarm state
// machine never depends on notifications.
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// maxSnapshotBytes bounds how much of a Frigate snapshot is buffered for an
// ntfy attachment upload (ntfy.sh itself caps attachments at 2 MB).
const maxSnapshotBytes = 10 << 20

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

	// ntfy has no multipart publish API: a local-file attachment must be the
	// PUT request body (with the message fields as headers). The previous
	// multipart body was stored verbatim as an attachment literally named
	// "attachment.bin" and the message/title were lost (incident 2026-09-24).
	snapshot, snapshotErr := n.fetchSnapshot(reviewID)
	var file []byte
	if snapshot != nil {
		file, err = io.ReadAll(io.LimitReader(snapshot, maxSnapshotBytes))
		_ = snapshot.Close()
		if err != nil {
			slog.Warn("snapshot read failed, sending text-only", "error", err)
			file = nil
		}
	} else if snapshotErr != nil {
		slog.Warn("snapshot unavailable, sending text-only", "error", snapshotErr)
	}

	if len(file) > 0 {
		req, err = n.fileRequest(payload, title, file)
	} else {
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

// fileRequest builds the ntfy publish for a message carrying a local-file
// attachment. ntfy expects the file as the PUT body and the message fields as
// headers (there is no multipart form API). Passing the bytes (not a stream)
// lets net/http set Content-Length, which ntfy's attachment path expects.
func (n *Notifier) fileRequest(payload map[string]any, title string, file []byte) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodPut, n.url, bytes.NewReader(file))
	if err != nil {
		return nil, err
	}
	// The message rides in a header, and HTTP header values cannot contain
	// newlines: collapse whitespace (a notification with an attachment loses
	// the paragraph breaks the text-only JSON path keeps).
	if msg, ok := payload["message"].(string); ok {
		req.Header.Set("X-Message", strings.Join(strings.Fields(msg), " "))
	}
	if title != "" {
		req.Header.Set("X-Title", title)
	}
	if p, ok := payload["priority"].(int); ok {
		req.Header.Set("X-Priority", strconv.Itoa(p))
	}
	req.Header.Set("X-Filename", "snapshot.jpg")
	return req, nil
}
