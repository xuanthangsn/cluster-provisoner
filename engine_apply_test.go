package engine_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sim.dev/engine"
	"sim.dev/engine/clock"
	"sim.dev/engine/events"
	"sim.dev/engine/provider/fake"
)

var epoch = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

// rig is one engine wired to a fake provider and a fake clock, recording the
// events it streams. Tests assert on reports and on those events — never on
// which provider methods were called.
type rig struct {
	opts   engine.Options
	fake   *fake.Provider
	clock  *clock.Fake
	events []events.Event
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{fake: fake.New(), clock: clock.NewFake(epoch)}
	r.opts = engine.Options{
		Provider: r.fake,
		StateDir: filepath.Join(t.TempDir(), "state"),
		Clock:    r.clock,
		Events:   func(e events.Event) { r.events = append(r.events, e) },
	}
	return r
}

func (r *rig) apply(t *testing.T, manifest string) engine.ApplyReport {
	t.Helper()
	report, err := engine.Apply(context.Background(), write(t, manifest), r.opts)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	return report
}

func (r *rig) destroy(t *testing.T, name string) engine.DestroyReport {
	t.Helper()
	report, err := engine.Destroy(context.Background(), name, r.opts)
	if err != nil {
		t.Fatalf("destroy: %v", err)
	}
	return report
}

func (r *rig) types() []string {
	out := make([]string, 0, len(r.events))
	for _, e := range r.events {
		out = append(out, e.Type())
	}
	return out
}

func (r *rig) startedTasks() []string {
	var out []string
	for _, e := range r.events {
		if s, ok := e.(events.TaskStarted); ok {
			out = append(out, s.Task)
		}
	}
	return out
}

const twoTasks = `
apiVersion: sim.dev/v1alpha2
kind: Environment
name: ordered
machines:
  - { name: node01, image: ubuntu:24.04 }
tasks:
  second: { machine: node01, run: "echo second", needs: [first] }
  first:  { machine: node01, run: "echo first" }
`

func TestApplyProvisionsAMinimalEnvironment(t *testing.T) {
	r := newRig(t)
	report := r.apply(t, minimal)

	if report.Status != engine.StatusProvisioned {
		t.Fatalf("status = %s (%s), want provisioned", report.Status, report.Error)
	}
	if report.SchemaVersion != engine.ReportSchemaVersion {
		t.Errorf("schemaVersion = %q", report.SchemaVersion)
	}
	if report.Environment != "skeleton" || report.PlanHash == "" {
		t.Errorf("environment %q, plan hash %q", report.Environment, report.PlanHash)
	}
	if len(report.Tasks) != 1 || !report.Tasks[0].Succeeded() {
		t.Errorf("tasks = %+v, want one that succeeded", report.Tasks)
	}
}

func TestApplyEventsFollowTheLifecycle(t *testing.T) {
	r := newRig(t)
	r.apply(t, minimal)

	want := "apply.started,network.created,machine.created,machine.ready," +
		"task.started,task.output,task.finished,apply.finished"
	if got := strings.Join(r.types(), ","); got != want {
		t.Errorf("events:\n got  %s\n want %s", got, want)
	}
	if fin := r.events[len(r.events)-1].(events.ApplyFinished); fin.Status != engine.StatusProvisioned {
		t.Errorf("apply.finished carries %s", fin.Status)
	}
}

func TestTasksRunInThePlansOrder(t *testing.T) {
	r := newRig(t)
	if report := r.apply(t, twoTasks); report.Status != engine.StatusProvisioned {
		t.Fatalf("status = %s (%s)", report.Status, report.Error)
	}
	// "second" is declared first but needs "first".
	if got := strings.Join(r.startedTasks(), ","); got != "first,second" {
		t.Errorf("tasks started in order %s, want first,second", got)
	}
}

func TestTaskOutputIsStreamedAsEvents(t *testing.T) {
	r := newRig(t)
	r.apply(t, minimal)

	for _, e := range r.events {
		if out, ok := e.(events.TaskOutput); ok {
			if out.Task != "greet" || out.Stream != events.StreamStdout || out.Data != "hello\n" {
				t.Errorf("output event = %+v", out)
			}
			return
		}
	}
	t.Error("no task.output event")
}

func TestAFailedTaskFailsTheBuildAndStopsIt(t *testing.T) {
	const manifest = `
apiVersion: sim.dev/v1alpha2
kind: Environment
name: failing
machines: [{ name: node01, image: ubuntu:24.04 }]
tasks:
  breaks: { machine: node01, run: "exit 3" }
  never:  { machine: node01, run: "true", needs: [breaks] }
`
	r := newRig(t)
	report := r.apply(t, manifest)

	if report.Status != engine.StatusTaskFailed {
		t.Fatalf("status = %s, want %s", report.Status, engine.StatusTaskFailed)
	}
	if !strings.Contains(report.Error, "breaks") || !strings.Contains(report.Error, "exit code 3") {
		t.Errorf("error does not say which Task failed or why: %q", report.Error)
	}
	if got := strings.Join(r.startedTasks(), ","); got != "breaks" {
		t.Errorf("tasks started: %s, want only breaks", got)
	}
}

func TestABackendFailureIsAProvisioningError(t *testing.T) {
	r := newRig(t)
	r.fake.InjectError(fake.OpCreateMachine, errors.New("out of disk"))

	report := r.apply(t, minimal)
	if report.Status != engine.StatusProvisioningError {
		t.Fatalf("status = %s, want %s", report.Status, engine.StatusProvisioningError)
	}
	if !strings.Contains(report.Error, "node01") || !strings.Contains(report.Error, "out of disk") {
		t.Errorf("error = %q", report.Error)
	}
	if len(report.Tasks) != 0 {
		t.Errorf("tasks ran despite the machine failing: %+v", report.Tasks)
	}
}

func TestAnInvalidManifestIsReportedAndNothingIsBuilt(t *testing.T) {
	r := newRig(t)
	report := r.apply(t, "kind: Environment\n")

	if report.Status != engine.StatusInvalid || len(report.Errors) == 0 {
		t.Fatalf("status = %s, errors %v", report.Status, report.Errors)
	}
	if len(r.events) != 0 {
		t.Errorf("an invalid manifest produced events: %v", r.types())
	}
	if _, err := os.Stat(r.opts.StateDir); !os.IsNotExist(err) {
		t.Errorf("an invalid manifest wrote state: %v", err)
	}
}

func TestApplyRefusesAnEnvironmentThatAlreadyExists(t *testing.T) {
	r := newRig(t)
	r.apply(t, minimal)

	report := r.apply(t, minimal)
	if report.Status != engine.StatusProvisioningError || !strings.Contains(report.Error, "already exists") {
		t.Errorf("second apply: status %s, error %q", report.Status, report.Error)
	}
}

func TestTheRunLogIsTheEventStream(t *testing.T) {
	r := newRig(t)
	report := r.apply(t, minimal)

	f, err := os.Open(report.RunLog)
	if err != nil {
		t.Fatalf("opening run log: %v", err)
	}
	defer f.Close()

	var logged []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var line struct{ Type string }
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatalf("run log line is not JSON: %s", sc.Text())
		}
		logged = append(logged, line.Type)
	}
	if got, want := strings.Join(logged, ","), strings.Join(r.types(), ","); got != want {
		t.Errorf("run log differs from the stream:\n log    %s\n stream %s", got, want)
	}
}

func TestApplyRecordsThePlan(t *testing.T) {
	r := newRig(t)
	report := r.apply(t, minimal)

	data, err := os.ReadFile(filepath.Join(r.opts.StateDir, "skeleton", "plan.json"))
	if err != nil {
		t.Fatalf("plan not recorded: %v", err)
	}
	var plan struct{ Hash string }
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Hash != report.PlanHash {
		t.Errorf("recorded hash %q, reported %q", plan.Hash, report.PlanHash)
	}
}

func TestEventTimesComeFromTheInjectedClock(t *testing.T) {
	r := newRig(t)
	r.apply(t, minimal)

	for _, e := range r.events {
		line, _ := events.Marshal(e)
		var v struct{ Time time.Time }
		if err := json.Unmarshal(line, &v); err != nil {
			t.Fatal(err)
		}
		if !v.Time.Equal(epoch) {
			t.Errorf("%s carries %v, but the injected clock reads %v", e.Type(), v.Time, epoch)
		}
	}
}

func TestDestroyRemovesEverything(t *testing.T) {
	r := newRig(t)
	r.apply(t, minimal)
	r.events = nil

	report := r.destroy(t, "skeleton")
	if report.Status != engine.StatusDestroyed {
		t.Fatalf("status = %s (%s)", report.Status, report.Error)
	}

	var removed []string
	for _, res := range report.Removed {
		removed = append(removed, res.Kind+"/"+res.Name)
	}
	if got := strings.Join(removed, ","); got != "machine/node01,network/default" {
		t.Errorf("removed = %s", got)
	}
	if _, err := os.Stat(filepath.Join(r.opts.StateDir, "skeleton")); !os.IsNotExist(err) {
		t.Errorf("state directory survived destroy: %v", err)
	}

	// After destroy, the same Environment builds again from nothing.
	if again := r.apply(t, minimal); again.Status != engine.StatusProvisioned {
		t.Errorf("rebuilding after destroy: %s (%s)", again.Status, again.Error)
	}
}

func TestDestroyOfNothingSucceeds(t *testing.T) {
	r := newRig(t)
	if report := r.destroy(t, "never-existed"); report.Status != engine.StatusDestroyed {
		t.Errorf("status = %s (%s)", report.Status, report.Error)
	}
}

func TestDestroyIsIdempotent(t *testing.T) {
	r := newRig(t)
	r.apply(t, minimal)
	for i := 0; i < 2; i++ {
		if report := r.destroy(t, "skeleton"); report.Status != engine.StatusDestroyed {
			t.Fatalf("destroy #%d: %s (%s)", i+1, report.Status, report.Error)
		}
	}
}

func TestADestroyThatFailsKeepsTheState(t *testing.T) {
	r := newRig(t)
	r.apply(t, minimal)
	r.fake.InjectError(fake.OpDeleteMachine, errors.New("busy"))

	report := r.destroy(t, "skeleton")
	if report.Status != engine.StatusProvisioningError {
		t.Fatalf("status = %s", report.Status)
	}
	if _, err := os.Stat(filepath.Join(r.opts.StateDir, "skeleton")); err != nil {
		t.Errorf("state was removed although the Environment was not: %v", err)
	}
}

func TestApplyAndDestroyNeedAProviderAndAStateDirectory(t *testing.T) {
	path := write(t, minimal)
	for name, opts := range map[string]engine.Options{
		"no provider":        {StateDir: t.TempDir()},
		"no state directory": {Provider: fake.New()},
	} {
		if _, err := engine.Apply(context.Background(), path, opts); err == nil {
			t.Errorf("apply with %s was accepted", name)
		}
		if _, err := engine.Destroy(context.Background(), "x", opts); err == nil {
			t.Errorf("destroy with %s was accepted", name)
		}
	}
}
