package alarm

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// SaveState atomically persists the alarm state to disk so that a restart
// reconstructs the exact state and every deadline.
func SaveState(path string, d Data) error {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".state-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }() // no-op after a successful rename
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil { // operators read state.json on the host
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// LoadState reads a previously persisted state.
func LoadState(path string) (Data, error) {
	// #nosec G304 -- the path comes from configuration (STATE_FILE), not user input.
	b, err := os.ReadFile(path)
	if err != nil {
		return Data{}, err
	}
	var d Data
	if err := json.Unmarshal(b, &d); err != nil {
		return Data{}, err
	}
	return d, nil
}

// IsExpired reports whether the deadline is set and already past.
func IsExpired(deadline time.Time, now time.Time) bool {
	return !deadline.IsZero() && !now.Before(deadline)
}

// HasDeadline reports whether a deadline is set (zero means none).
func HasDeadline(deadline time.Time) bool {
	return !deadline.IsZero()
}

// ErrNoState is returned when no state file exists yet.
var ErrNoState = errors.New("no state file")
