package alarm

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveLoadStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	now := time.Date(2026, 9, 20, 21, 0, 0, 0, time.UTC)
	d := Data{
		State:              StateTriggered,
		Updated:            now,
		TriggeredAt:        now,
		EscalationDeadline: now.Add(time.Minute),
		Flashing:           true,
	}

	if err := SaveState(path, d); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	got, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if got.State != StateTriggered || !got.Flashing {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if !got.EscalationDeadline.Equal(d.EscalationDeadline) {
		t.Fatalf("deadline mismatch: %v != %v", got.EscalationDeadline, d.EscalationDeadline)
	}
}

func TestLoadMissingState(t *testing.T) {
	if _, err := LoadState(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected error for missing state file")
	}
}

func TestIsExpired(t *testing.T) {
	now := time.Now()
	if IsExpired(time.Time{}, now) {
		t.Fatal("zero deadline must not be expired")
	}
	if IsExpired(now.Add(time.Hour), now) {
		t.Fatal("future deadline must not be expired")
	}
	if !IsExpired(now.Add(-time.Second), now) {
		t.Fatal("past deadline must be expired")
	}
}

func TestLoadStateKeepsDirReadable(t *testing.T) {
	// The state dir should remain readable for operators.
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := SaveState(path, Data{State: StateDisarmed}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o444 == 0 {
		t.Fatalf("state file not world-readable: %v", info.Mode())
	}
}
