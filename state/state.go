// Package state owns the Environment state directory on the host: the Plan an
// Environment was built from and the log of every run.
//
// Everything is created owner-only from the start — directories 0700, files
// 0600 — rather than tightened afterwards, so there is no window in which a
// secret written here is readable by another user (NFR-11).
//
// The state directory is a record, not the source of truth. What exists is
// whatever the Machine provider reports; the engine never relies on this
// directory to find resources, so losing it never strands them (NFR-7).
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sim.dev/engine/planner"
)

// Permissions every state path is created with.
const (
	dirMode  = 0o700
	fileMode = 0o600
)

// Dir is a state directory root holding one subdirectory per Environment.
type Dir struct {
	root string
}

// New returns the state directory rooted at root.
func New(root string) Dir { return Dir{root: root} }

// DefaultRoot is $XDG_STATE_HOME/simctl, falling back to
// ~/.local/state/simctl.
func DefaultRoot() (string, error) {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "simctl"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding a state directory: %w", err)
	}
	return filepath.Join(home, ".local", "state", "simctl"), nil
}

// Root returns the root path.
func (d Dir) Root() string { return d.root }

// Path returns an Environment's directory. It refuses any name that is not a
// single plain path component, so that no manifest can direct writes outside
// the root.
func (d Dir) Path(env string) (string, error) {
	if d.root == "" {
		return "", errors.New("no state directory is configured")
	}
	if env == "" || env == "." || env == ".." ||
		strings.ContainsAny(env, `/\`+"\x00") || filepath.Base(env) != env {
		return "", fmt.Errorf("%q cannot name a state directory", env)
	}
	return filepath.Join(d.root, env), nil
}

// Exists reports whether an Environment has a state directory.
func (d Dir) Exists(env string) (bool, error) {
	path, err := d.Path(env)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	}
	return false, err
}

// Create makes an Environment's directory, owner-only.
func (d Dir) Create(env string) error {
	path, err := d.Path(env)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d.root, dirMode); err != nil {
		return fmt.Errorf("creating state root: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(path, "runs"), dirMode); err != nil {
		return fmt.Errorf("creating state for %s: %w", env, err)
	}
	// MkdirAll applies the umask; set the mode explicitly so it is exact.
	for _, p := range []string{path, filepath.Join(path, "runs")} {
		if err := os.Chmod(p, dirMode); err != nil {
			return err
		}
	}
	return nil
}

// WritePlan records the Plan an Environment is built from. The write is
// atomic: a reader sees the old Plan or the new one, never part of either.
func (d Dir) WritePlan(env string, plan *planner.Plan) error {
	path, err := d.Path(env)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(path, "plan.json"), append(data, '\n'))
}

// CreateRunLog opens a new, empty run log named after at, owner-only. It
// returns the open file and its path.
func (d Dir) CreateRunLog(env string, at time.Time) (*os.File, string, error) {
	path, err := d.Path(env)
	if err != nil {
		return nil, "", err
	}
	stamp := at.UTC().Format("20060102T150405.000000000Z")
	for i := 0; i < 1000; i++ {
		name := stamp
		if i > 0 {
			name = fmt.Sprintf("%s-%d", stamp, i)
		}
		logPath := filepath.Join(path, "runs", name+".jsonl")
		f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return nil, "", fmt.Errorf("creating run log: %w", err)
		}
		return f, logPath, nil
	}
	return nil, "", fmt.Errorf("creating run log: too many runs at %s", stamp)
}

// Remove deletes an Environment's directory. Removing one that does not exist
// succeeds.
func (d Dir) Remove(env string) error {
	path, err := d.Path(env)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("removing state for %s: %w", env, err)
	}
	return nil
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*") // created 0600
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(fileMode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
