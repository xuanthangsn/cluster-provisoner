package engine_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"sim.dev/engine"
	"sim.dev/engine/planner"
)

const minimal = `
apiVersion: sim.dev/v1alpha2
kind: Environment
name: skeleton
machines:
  - name: node01
    image: ubuntu:24.04
tasks:
  greet:
    machine: node01
    run: echo hello
checks:
  greeted:
    machine: node01
    run: test -f /tmp/hello
`

// write puts a manifest in its own directory and returns its path.
func write(t *testing.T, manifest string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "environment.yaml")
	if err := os.WriteFile(path, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestValidateReportsAValidManifest(t *testing.T) {
	report, err := engine.Validate(write(t, minimal))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !report.Valid {
		t.Fatalf("manifest reported invalid: %v", report.Errors)
	}
	if report.SchemaVersion != engine.ReportSchemaVersion {
		t.Errorf("schemaVersion = %q, want %q", report.SchemaVersion, engine.ReportSchemaVersion)
	}
	if want := (engine.Summary{Machines: 1, Tasks: 1, Checks: 1}); report.Summary != want {
		t.Errorf("summary = %+v, want %+v", report.Summary, want)
	}
	if report.Environment == nil {
		t.Fatal("report carries no Environment")
	}
	// The defaulted manifest is what the engine works from, so a caller must
	// be able to see it (FR-2).
	if report.Environment.Backend != "container" {
		t.Errorf("defaulted backend = %q, want container", report.Environment.Backend)
	}
}

func TestValidateReportsProblemsWithoutAnError(t *testing.T) {
	// A manifest problem belongs in the report. The error return is for
	// failing to reach the file at all.
	report, err := engine.Validate(write(t, "kind: Environment\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.Valid {
		t.Error("an incomplete manifest was reported valid")
	}
	if len(report.Errors) == 0 {
		t.Error("no problems reported")
	}
	if report.SchemaVersion != engine.ReportSchemaVersion {
		t.Errorf("an invalid manifest's report carries no schema version")
	}
}

func TestValidateErrorsWhenTheManifestCannotBeRead(t *testing.T) {
	_, err := engine.Validate(filepath.Join(t.TempDir(), "absent.yaml"))
	if err == nil {
		t.Fatal("expected an error for a missing manifest")
	}
}

func TestPlanDescribesWhatApplyWouldDo(t *testing.T) {
	report, err := engine.Plan(write(t, minimal), planner.Passthrough{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !report.Valid {
		t.Fatalf("planning failed: %v", report.Errors)
	}
	if report.SchemaVersion != engine.ReportSchemaVersion {
		t.Errorf("schemaVersion = %q, want %q", report.SchemaVersion, engine.ReportSchemaVersion)
	}
	if report.Plan == nil {
		t.Fatal("report carries no Plan")
	}
	if report.Plan.Hash == "" {
		t.Error("Plan carries no hash")
	}
	if len(report.Plan.Machines) != 1 || report.Plan.Machines[0].IP == "" {
		t.Errorf("machines = %+v, want one with an allocated address", report.Plan.Machines)
	}
	if len(report.Plan.Tasks) != 1 {
		t.Errorf("tasks = %d, want 1", len(report.Plan.Tasks))
	}
}

func TestPlanRefusesAnInvalidManifest(t *testing.T) {
	report, err := engine.Plan(write(t, "kind: Environment\n"), planner.Passthrough{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Valid {
		t.Error("an invalid manifest was planned")
	}
	if report.Plan != nil {
		t.Error("a Plan was returned for an invalid manifest")
	}
	if len(report.Errors) == 0 {
		t.Error("no problems reported")
	}
}

// TestPlanHasNoSideEffects is FR-7: a plan can be taken on a live system
// safely, so planning must create nothing and write no state.
func TestPlanHasNoSideEffects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "environment.yaml")
	if err := os.WriteFile(path, []byte(minimal), 0o644); err != nil {
		t.Fatal(err)
	}

	before := snapshot(t, dir)

	if _, err := engine.Plan(path, planner.Passthrough{}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Validate(path); err != nil {
		t.Fatal(err)
	}

	after := snapshot(t, dir)
	if len(before) != len(after) {
		t.Errorf("planning changed the directory\n before: %v\n after:  %v", before, after)
		return
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("planning changed the directory\n before: %v\n after:  %v", before, after)
			return
		}
	}
}

// TestValidateAndPlanDoNotNeedDocker holds FR-3 and NFR-10 honest: these
// operations must work on a host that cannot run the Environment. The test
// removes any executable from PATH and points DOCKER_HOST at a dead port, so
// anything shelling out to docker, or dialling it, would fail here.
func TestValidateAndPlanDoNotNeedDocker(t *testing.T) {
	path := write(t, minimal)

	t.Setenv("PATH", "")
	t.Setenv("DOCKER_HOST", "tcp://127.0.0.1:1")

	validated, err := engine.Validate(path)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !validated.Valid {
		t.Fatalf("validate reported problems: %v", validated.Errors)
	}

	planned, err := engine.Plan(path, planner.Passthrough{})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if !planned.Valid {
		t.Fatalf("plan reported problems: %v", planned.Errors)
	}
}

func TestPlanIsStableAcrossCalls(t *testing.T) {
	path := write(t, minimal)

	first, err := engine.Plan(path, planner.Passthrough{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		again, err := engine.Plan(path, planner.Passthrough{})
		if err != nil {
			t.Fatal(err)
		}
		if again.Plan.Hash != first.Plan.Hash {
			t.Fatalf("plan hash changed between calls: %q then %q", first.Plan.Hash, again.Plan.Hash)
		}
	}
}

// snapshot lists a directory tree, so a test can assert nothing in it moved.
func snapshot(t *testing.T, dir string) []string {
	t.Helper()

	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		out = append(out, rel+" "+info.Mode().String()+" "+itoa(info.Size()))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
