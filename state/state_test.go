package state_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"sim.dev/engine/planner"
	"sim.dev/engine/state"
)

var at = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func TestEverythingIsOwnerOnlyFromCreation(t *testing.T) {
	// A permissive umask must not loosen what the state directory creates.
	old := syscall.Umask(0)
	defer syscall.Umask(old)

	root := filepath.Join(t.TempDir(), "state")
	d := state.New(root)
	if err := d.Create("env"); err != nil {
		t.Fatal(err)
	}
	if err := d.WritePlan("env", &planner.Plan{Environment: "env"}); err != nil {
		t.Fatal(err)
	}
	f, _, err := d.CreateRunLog("env", at)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	err = filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		want := fs.FileMode(0o600)
		if e.IsDir() {
			want = 0o700
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s has mode %v, want %v", path, got, want)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWritePlanRecordsThePlan(t *testing.T) {
	d := state.New(t.TempDir())
	if err := d.Create("env"); err != nil {
		t.Fatal(err)
	}
	if err := d.WritePlan("env", &planner.Plan{Environment: "env", Hash: "abc"}); err != nil {
		t.Fatal(err)
	}

	path, _ := d.Path("env")
	data, err := os.ReadFile(filepath.Join(path, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	var p planner.Plan
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	if p.Hash != "abc" {
		t.Errorf("recorded hash = %q, want abc", p.Hash)
	}
}

func TestRunLogsNeverOverwriteEachOther(t *testing.T) {
	d := state.New(t.TempDir())
	if err := d.Create("env"); err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		f, path, err := d.CreateRunLog("env", at) // the same instant each time
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
		if seen[path] {
			t.Fatalf("run log %s was handed out twice", path)
		}
		seen[path] = true
	}
}

func TestRemoveDeletesEverythingAndToleratesAbsence(t *testing.T) {
	d := state.New(t.TempDir())
	if err := d.Create("env"); err != nil {
		t.Fatal(err)
	}
	if err := d.Remove("env"); err != nil {
		t.Fatal(err)
	}
	if exists, _ := d.Exists("env"); exists {
		t.Error("state still exists after remove")
	}
	if err := d.Remove("env"); err != nil {
		t.Errorf("removing absent state must succeed: %v", err)
	}
}

func TestNamesCannotEscapeTheRoot(t *testing.T) {
	d := state.New(t.TempDir())
	for _, name := range []string{"", ".", "..", "../escape", "a/b", `a\b`, "nul\x00"} {
		if _, err := d.Path(name); err == nil {
			t.Errorf("%q was accepted as an Environment name", name)
		}
	}
}
