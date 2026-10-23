package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sim.dev/engine"
	"sim.dev/engine/clock"
	"sim.dev/engine/events"
	"sim.dev/engine/internal/exitcode"
	"sim.dev/engine/provider"
	"sim.dev/engine/provider/fake"
)

var epoch = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

// testDeps wires simctl to one shared fake provider, a fixed clock and a
// temporary state directory.
func testDeps(t *testing.T) (deps, *fake.Provider) {
	t.Helper()
	p := fake.New()
	dir := filepath.Join(t.TempDir(), "state")
	return deps{
		provider: func() (provider.Provider, error) { return p, nil },
		stateDir: func() (string, error) { return dir, nil },
		clock:    clock.NewFake(epoch),
	}, p
}

func invokeWith(d deps, args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := runWith(args, &stdout, &stderr, d)
	return code, stdout.String(), stderr.String()
}

const failing = `
apiVersion: sim.dev/v1alpha2
kind: Environment
name: failing
machines: [{ name: node01, image: ubuntu:24.04 }]
tasks:
  breaks: { machine: node01, run: "exit 3" }
`

func TestApplyAndDestroyExitCodes(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*fake.Provider)
		args  func(t *testing.T) []string
		want  int
	}{
		{"a provisioned Environment", nil,
			func(t *testing.T) []string { return []string{"apply", "-f", write(t, minimal)} }, exitcode.OK},
		{"an invalid manifest", nil,
			func(t *testing.T) []string { return []string{"apply", "-f", write(t, "kind: Environment\n")} }, exitcode.InvalidManifest},
		{"a failed Task", nil,
			func(t *testing.T) []string { return []string{"apply", "-f", write(t, failing)} }, exitcode.TaskFailed},
		{"a backend failure", func(p *fake.Provider) { p.InjectError(fake.OpCreateNetwork, errors.New("no space")) },
			func(t *testing.T) []string { return []string{"apply", "-f", write(t, minimal)} }, exitcode.ProvisioningError},
		{"apply without -f", nil,
			func(*testing.T) []string { return []string{"apply"} }, exitcode.Usage},
		{"destroy of nothing", nil,
			func(*testing.T) []string { return []string{"destroy", "never-existed"} }, exitcode.OK},
		{"destroy without a name", nil,
			func(*testing.T) []string { return []string{"destroy"} }, exitcode.Usage},
		{"destroy when the backend fails", func(p *fake.Provider) { p.InjectError(fake.OpList, errors.New("down")) },
			func(*testing.T) []string { return []string{"destroy", "skeleton"} }, exitcode.ProvisioningError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, p := testDeps(t)
			if tc.setup != nil {
				tc.setup(p)
			}
			code, stdout, stderr := invokeWith(d, tc.args(t)...)
			if code != tc.want {
				t.Errorf("exit code = %d, want %d\nstdout: %s\nstderr: %s", code, tc.want, stdout, stderr)
			}
		})
	}
}

func TestDestroyAcceptsFlagsAfterTheName(t *testing.T) {
	d, _ := testDeps(t)
	invokeWith(d, "apply", "-f", write(t, minimal))

	code, stdout, _ := invokeWith(d, "destroy", "skeleton", "--json")
	if code != exitcode.OK {
		t.Fatalf("exit code %d", code)
	}
	if !strings.Contains(stdout, `"type":"destroy.report"`) {
		t.Errorf("--json after the name was ignored:\n%s", stdout)
	}
}

// TestApplyJSONIsTheLibrarysEventStream holds "no second event path": the
// lines simctl prints are byte for byte what the library's own JSON-lines
// rendering of the same build produces.
func TestApplyJSONIsTheLibrarysEventStream(t *testing.T) {
	path := write(t, minimal)

	d, _ := testDeps(t)
	code, stdout, stderr := invokeWith(d, "apply", "-f", path, "--json")
	if code != exitcode.OK {
		t.Fatalf("exit code %d: %s", code, stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	cliEvents, reportLine := lines[:len(lines)-1], lines[len(lines)-1]

	var library bytes.Buffer
	_, err := engine.Apply(context.Background(), path, engine.Options{
		Provider: fake.New(),
		StateDir: filepath.Join(t.TempDir(), "state"),
		Clock:    clock.NewFake(epoch),
		Events:   events.JSONLines(&library, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(cliEvents, "\n")+"\n", library.String(); got != want {
		t.Errorf("simctl's events differ from the library's\n simctl:\n%s\n library:\n%s", got, want)
	}

	var report struct {
		Type   string
		Status string
	}
	if err := json.Unmarshal([]byte(reportLine), &report); err != nil {
		t.Fatalf("the last line is not the report: %v\n%s", err, reportLine)
	}
	if report.Type != "apply.report" || report.Status != engine.StatusProvisioned {
		t.Errorf("report line = %s", reportLine)
	}
}

func TestEveryJSONLineIsAnObjectWithAType(t *testing.T) {
	d, _ := testDeps(t)
	path := write(t, minimal)
	_, applied, _ := invokeWith(d, "apply", "-f", path, "--json")
	_, destroyed, _ := invokeWith(d, "destroy", "skeleton", "--json")

	for _, line := range strings.Split(strings.TrimSpace(applied+destroyed), "\n") {
		var v struct{ Type string }
		if err := json.Unmarshal([]byte(line), &v); err != nil || v.Type == "" {
			t.Errorf("not a typed JSON object: %s", line)
		}
	}
}

func TestHumanApplyOutputTellsTheStory(t *testing.T) {
	d, _ := testDeps(t)
	_, stdout, _ := invokeWith(d, "apply", "-f", write(t, minimal))

	for _, want := range []string{
		"Applying skeleton", "network  default", "node01", "ready at 172.30.0.2",
		"task   1/1  greet on node01  ok", "Environment skeleton provisioned.", "Run log:",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output does not mention %q\n%s", want, stdout)
		}
	}
	// A succeeding Task's output is held back.
	if strings.Contains(stdout, "| hello") {
		t.Errorf("a healthy Task's output was printed:\n%s", stdout)
	}
}

func TestHumanOutputShowsAFailedTasksReasonAndOutput(t *testing.T) {
	d, p := testDeps(t)
	p.SetHandler(func(_ fake.Machine, req provider.ExecRequest) (provider.ExecResult, bool) {
		return provider.ExecResult{ExitCode: 3, Stderr: []byte("kubelet: config not found\n")}, true
	})
	_, stdout, _ := invokeWith(d, "apply", "-f", write(t, failing))

	for _, want := range []string{"breaks on node01  FAILED", "exit code 3, expected 0", "| kubelet: config not found",
		"simctl destroy failing"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output does not mention %q\n%s", want, stdout)
		}
	}
}

func TestHumanDestroyOutput(t *testing.T) {
	d, _ := testDeps(t)
	invokeWith(d, "apply", "-f", write(t, minimal))

	_, stdout, _ := invokeWith(d, "destroy", "skeleton")
	for _, want := range []string{"Destroying skeleton", "removed  machine  node01", "removed  network  default",
		"Environment skeleton destroyed."} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output does not mention %q\n%s", want, stdout)
		}
	}

	_, stdout, _ = invokeWith(d, "destroy", "skeleton")
	if !strings.Contains(stdout, "Nothing to destroy") {
		t.Errorf("a second destroy should say there was nothing:\n%s", stdout)
	}
}
