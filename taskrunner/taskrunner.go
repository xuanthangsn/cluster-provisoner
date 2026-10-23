// Package taskrunner runs one Task to a verdict.
//
// In this release a verdict rests on criterion 1 alone: the command's exit
// code and, when the Task gives one, its stdout condition. Probes and wait
// Tasks are issue 05, attempts and onFailure issue 06, background Tasks
// issue 07.
package taskrunner

import (
	"context"
	"fmt"
	"time"

	"sim.dev/engine/clock"
	"sim.dev/engine/events"
	"sim.dev/engine/planner"
	"sim.dev/engine/provider"
)

// Shell runs a Task's command. The manifest design specifies that a Task's
// run field is inline bash.
var Shell = []string{"bash", "-c"}

// Result is one Task's verdict.
type Result struct {
	Task     string `json:"task"`
	Machine  string `json:"machine"`
	Status   string `json:"status"`
	ExitCode int    `json:"exitCode"`
	Duration string `json:"duration"`
	// Reason explains a verdict other than succeeded.
	Reason string `json:"reason,omitempty"`
}

// Succeeded reports whether the Task succeeded.
func (r Result) Succeeded() bool { return r.Status == events.StatusSucceeded }

// Runner runs Tasks on a provider.
type Runner struct {
	Provider provider.Provider
	Clock    clock.Clock
	Emit     events.Sink
}

// Run runs one Task. index counts from 1 within total.
//
// A Task whose command ran but did not meet its criterion fails. A Task whose
// command could not be run at all — the backend failed — ends in error, which
// is a provisioning problem rather than a verdict on the Task.
func (r *Runner) Run(ctx context.Context, env string, t planner.Task, index, total int) Result {
	started := r.Clock.Now()
	r.emit(events.TaskStarted{
		Time: started, Environment: env, Task: t.Name, Machine: t.Machine, Index: index, Total: total,
	})

	result := r.run(ctx, env, t)
	result.Duration = r.Clock.Now().Sub(started).String()

	r.emit(events.TaskFinished{
		Time: r.Clock.Now(), Environment: env, Task: t.Name, Machine: t.Machine,
		Status: result.Status, ExitCode: result.ExitCode, Duration: result.Duration, Reason: result.Reason,
	})
	return result
}

func (r *Runner) run(ctx context.Context, env string, t planner.Task) Result {
	res := Result{Task: t.Name, Machine: t.Machine}

	timeout, err := time.ParseDuration(t.Timeout)
	if err != nil || timeout <= 0 {
		res.Status = events.StatusError
		res.Reason = fmt.Sprintf("the Plan gives an unusable timeout %q", t.Timeout)
		return res
	}

	out, err := r.Provider.Exec(ctx, provider.ExecRequest{
		Machine: provider.MachineRef{Environment: env, Machine: t.Machine},
		Argv:    append(append([]string(nil), Shell...), t.Run),
		Timeout: timeout,
		Stdout:  r.output(env, t, events.StreamStdout),
		Stderr:  r.output(env, t, events.StreamStderr),
	})
	if err != nil {
		res.Status = events.StatusError
		res.Reason = fmt.Sprintf("the command could not be run: %v", err)
		return res
	}

	res.ExitCode = out.ExitCode
	switch {
	case out.TimedOut:
		res.Status = events.StatusFailed
		res.Reason = fmt.Sprintf("did not finish within %s", timeout)
	case out.ExitCode != t.ExitCode:
		res.Status = events.StatusFailed
		res.Reason = fmt.Sprintf("exit code %d, expected %d", out.ExitCode, t.ExitCode)
	default:
		if ok, why := t.Stdout.Test(out.Stdout); !ok {
			res.Status = events.StatusFailed
			res.Reason = why
			return res
		}
		res.Status = events.StatusSucceeded
	}
	return res
}

// output returns a writer turning each chunk of a stream into a TaskOutput
// event.
func (r *Runner) output(env string, t planner.Task, stream string) *eventWriter {
	return &eventWriter{emit: func(chunk string) {
		r.emit(events.TaskOutput{
			Time: r.Clock.Now(), Environment: env, Task: t.Name, Machine: t.Machine, Stream: stream, Data: chunk,
		})
	}}
}

func (r *Runner) emit(e events.Event) {
	if r.Emit != nil {
		r.Emit(e)
	}
}

type eventWriter struct {
	emit func(string)
}

func (w *eventWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		w.emit(string(p)) // string() copies, so the caller may reuse p
	}
	return len(p), nil
}
