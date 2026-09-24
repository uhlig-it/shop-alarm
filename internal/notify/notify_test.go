package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A local-file snapshot must go out as a PUT with the file as the body and the
// message fields as headers — ntfy has no multipart publish API. Regression
// for the 2026-09-24 incident: the multipart body was stored verbatim as an
// attachment literally named "attachment.bin" and the message/title were
// dropped, so every alert arrived as "You received a file: attachment.bin".
func TestNotifyAttachesSnapshotAsPutBody(t *testing.T) {
	type captured struct {
		method string
		header http.Header
		body   []byte
	}
	got := make(chan captured, 1)
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- captured{method: r.Method, header: r.Header.Clone(), body: b}
		w.WriteHeader(http.StatusOK)
	}))
	defer ntfy.Close()

	jpeg := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0xff, 0xd9}
	frigate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/review/rev-1/snapshot.jpg" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(jpeg)
	}))
	defer frigate.Close()

	n := New(Config{NTFYURL: ntfy.URL, LiveStreamURL: "http://live/", FrigateAPIURL: frigate.URL})
	n.Notify(5, "Werkstatt alarm triggered", "Source: entry_delay", "rev-1")

	c := <-got
	if c.method != http.MethodPut {
		t.Errorf("method = %s, want PUT", c.method)
	}
	if ct := c.header.Get("Content-Type"); strings.HasPrefix(ct, "multipart/") {
		t.Errorf("multipart content-type %q; ntfy needs the file as the PUT body", ct)
	}
	if v := c.header.Get("X-Title"); v != "Werkstatt alarm triggered" {
		t.Errorf("X-Title = %q", v)
	}
	if v := c.header.Get("X-Message"); v != "Source: entry_delay Live: http://live/" {
		t.Errorf("X-Message = %q (newlines must be collapsed: headers cannot carry them)", v)
	}
	if v := c.header.Get("X-Priority"); v != "5" {
		t.Errorf("X-Priority = %q, want 5", v)
	}
	if v := c.header.Get("X-Filename"); v != "snapshot.jpg" {
		t.Errorf("X-Filename = %q", v)
	}
	if string(c.body) != string(jpeg) {
		t.Errorf("body = %x, want the snapshot %x", c.body, jpeg)
	}
}

// Without a Frigate API (or review id) the message still goes out as JSON —
// the text-only path is unchanged and keeps its newlines.
func TestNotifyTextOnlyUsesJSONPost(t *testing.T) {
	type captured struct {
		method string
		ctype  string
		body   []byte
	}
	got := make(chan captured, 1)
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- captured{method: r.Method, ctype: r.Header.Get("Content-Type"), body: b}
		w.WriteHeader(http.StatusOK)
	}))
	defer ntfy.Close()

	n := New(Config{NTFYURL: ntfy.URL, LiveStreamURL: "http://live/"})
	n.Notify(4, "Arming refused", "Frigate is not online", "")

	c := <-got
	if c.method != http.MethodPost {
		t.Errorf("method = %s, want POST", c.method)
	}
	if !strings.HasPrefix(c.ctype, "application/json") {
		t.Errorf("content-type = %q, want application/json", c.ctype)
	}
	var payload map[string]any
	if err := json.Unmarshal(c.body, &payload); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if payload["title"] != "Arming refused" {
		t.Errorf("title = %v", payload["title"])
	}
	if payload["message"] != "Frigate is not online\n\nLive: http://live/" {
		t.Errorf("message = %v", payload["message"])
	}
}
