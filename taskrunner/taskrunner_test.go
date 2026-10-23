package taskrunner_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"sim.dev/engine/clock"
	"sim.dev/engine/events"
	"sim.dev/engine/planner"
	"sim.dev/engine/provider"
	"sim.dev/engine/provider/fake"
	"sim.dev/engine/spec"
	"sim.dev/engine/taskrunner"
)

func str(s string) *string { return &s }

// setup returns a runner on a fake with one running Machine, and the events
// it emits.
func setup(t *testing.T) (*taskrunner.Runner, *fake.Provider, *[]events.Event) {
	t.Helper()
	ctx := context.Background()
	p := fake.New()
	if err := p.CreateNetwork(ctx, provider.Network{Environment: "env", Name: "default", Subnet: "10.0.0.0/24"}); err != nil {
		t.Fatal(err)
	}
	m := provider.Machine{Environment: "env", Name: "node01", Image: "i", Network: "default", IP: "10.0.0.2"}
	if err := p.CreateMachine(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := p.StartMachine(ctx, m.Ref()); err != nil {
		t.Fatal(err)
	}

	var seen []events.Event
	r := &taskrunner.Runner{
		Provider: p,
		Clock:    clock.NewFake(time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)),
		Emit:     func(e events.Event) { seen = append(seen, e) },
	}
	return r, p, &seen
}

func task(run string) planner.Task {
	return planner.Task{Name: "t", Type: "exec", Machine: "node01", Run: run, Timeout: "1m0s", Attempts: 1}
}

func TestATaskSucceedsWhenItsExitCodeMatches(t *testing.T) {
	r, _, _ := setup(t)
	if res := r.Run(context.Background(), "env", task("true"), 1, 1); !res.Succeeded() {
		t.Errorf("status = %s (%s), want succeeded", res.Status, res.Reason)
	}
}

func TestATaskFailsOnAnUnexpectedExitCode(t *testing.T) {
	r, _, _ := setup(t)
	res := r.Run(context.Background(), "env", task("exit 3"), 1, 1)

	if res.Status != events.StatusFailed {
		t.Errorf("status = %s, want failed", res.Status)
	}
	if res.ExitCode != 3 || !strings.Contains(res.Reason, "exit code 3") {
		t.Errorf("exit code %d, reason %q", res.ExitCode, res.Reason)
	}
}

func TestAnExpectedNonZeroExitCodeSucceeds(t *testing.T) {
	r, _, _ := setup(t)
	tk := task("exit 3")
	tk.ExitCode = 3
	if res := r.Run(context.Background(), "env", tk, 1, 1); !res.Succeeded() {
		t.Errorf("status = %s (%s), want succeeded", res.Status, res.Reason)
	}
}

func TestTheStdoutConditionIsPartOfCriterionOne(t *testing.T) {
	cases := []struct {
		name  string
		match spec.Match
		want  string
	}{
		{"equals, ignoring the trailing newline", spec.Match{Equals: str("hello")}, events.StatusSucceeded},
		{"equals, mismatched", spec.Match{Equals: str("goodbye")}, events.StatusFailed},
		{"contains", spec.Match{Contains: str("ell")}, events.StatusSucceeded},
		{"matches", spec.Match{Matches: str("^h.*o$")}, events.StatusSucceeded},
		{"matches, mismatched", spec.Match{Matches: str("^x")}, events.StatusFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _, _ := setup(t)
			tk := task("echo hello")
			m := tc.match
			tk.Stdout = &m
			if res := r.Run(context.Background(), "env", tk, 1, 1); res.Status != tc.want {
				t.Errorf("status = %s (%s), want %s", res.Status, res.Reason, tc.want)
			}
		})
	}
}

func TestATimedOutCommandFails(t *testing.T) {
	r, p, _ := setup(t)
	p.SetHandler(func(fake.Machine, provider.ExecRequest) (provider.ExecResult, bool) {
		return provider.ExecResult{TimedOut: true}, true
	})
	res := r.Run(context.Background(), "env", task("sleep 999"), 1, 1)
	if res.Status != events.StatusFailed || !strings.Contains(res.Reason, "within") {
		t.Errorf("status = %s, reason %q", res.Status, res.Reason)
	}
}

func TestABackendFailureIsAnErrorNotAFailedTask(t *testing.T) {
	r, p, _ := setup(t)
	p.InjectError(fake.OpExec, errors.New("backend exploded"))

	res := r.Run(context.Background(), "env", task("true"), 1, 1)
	if res.Status != events.StatusError {
		t.Errorf("status = %s, want error", res.Status)
	}
}

func TestTheCommandRunsThroughBash(t *testing.T) {
	r, p, _ := setup(t)
	var argv []string
	p.SetHandler(func(_ fake.Machine, req provider.ExecRequest) (provider.ExecResult, bool) {
		argv = req.Argv
		return provider.ExecResult{}, true
	})
	r.Run(context.Background(), "env", task("echo $HOME"), 1, 1)

	if strings.Join(argv, "|") != "bash|-c|echo $HOME" {
		t.Errorf("argv = %q, want bash -c and the command unaltered", argv)
	}
}

func TestEventsBracketTheTaskAndCarryItsOutput(t *testing.T) {
	r, _, seen := setup(t)
	r.Run(context.Background(), "env", task("echo hello"), 2, 3)

	var types []string
	for _, e := range *seen {
		types = append(types, e.Type())
	}
	if got := strings.Join(types, ","); got != "task.started,task.output,task.finished" {
		t.Fatalf("events = %s", got)
	}

	started := (*seen)[0].(events.TaskStarted)
	if started.Index != 2 || started.Total != 3 {
		t.Errorf("position = %d/%d, want 2/3", started.Index, started.Total)
	}
	out := (*seen)[1].(events.TaskOutput)
	if out.Stream != events.StreamStdout || out.Data != "hello\n" {
		t.Errorf("output = %s %q", out.Stream, out.Data)
	}
	if fin := (*seen)[2].(events.TaskFinished); fin.Status != events.StatusSucceeded {
		t.Errorf("finished with %s", fin.Status)
	}
}
