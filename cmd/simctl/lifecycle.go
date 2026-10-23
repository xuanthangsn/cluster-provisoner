package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"sim.dev/engine"
	"sim.dev/engine/events"
	"sim.dev/engine/internal/exitcode"
)

// lifecycleFlags parses the flags apply and destroy share. Flags may come
// before or after the one positional argument destroy takes.
func lifecycleFlags(name string, args []string, stderr io.Writer) (file string, asJSON bool, stateDir string, positional []string, ok bool) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	f := fs.String("f", "", "path to the Environment manifest")
	j := fs.Bool("json", false, "print events and the report as JSON lines")
	s := fs.String("state-dir", "", "state directory root")

	for {
		if err := fs.Parse(args); err != nil {
			return "", false, "", nil, false
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
	return *f, *j, *s, positional, true
}

func (d deps) options(stateDir string, sink events.Sink) (engine.Options, error) {
	p, err := d.provider()
	if err != nil {
		return engine.Options{}, err
	}
	if stateDir == "" {
		if stateDir, err = d.stateDir(); err != nil {
			return engine.Options{}, err
		}
	}
	return engine.Options{Provider: p, StateDir: stateDir, Clock: d.clock, Events: sink}, nil
}

func cmdApply(args []string, stdout, stderr io.Writer, d deps) int {
	file, asJSON, stateDir, positional, ok := lifecycleFlags("apply", args, stderr)
	if !ok {
		return exitcode.Usage
	}
	if file == "" || len(positional) > 0 {
		fmt.Fprintln(stderr, "simctl apply: usage: simctl apply -f <manifest> [--json] [--state-dir <dir>]")
		return exitcode.Usage
	}

	sink := renderer(stdout, stderr, asJSON)
	opts, err := d.options(stateDir, sink)
	if err != nil {
		fmt.Fprintf(stderr, "simctl apply: %v\n", err)
		return exitcode.Usage
	}

	report, err := engine.Apply(context.Background(), file, opts)
	if err != nil {
		fmt.Fprintf(stderr, "simctl apply: %v\n", err)
		return exitcode.Usage
	}

	if asJSON {
		writeReportLine(stdout, "apply.report", report)
	} else {
		renderApplyReport(stdout, report)
	}

	switch report.Status {
	case engine.StatusProvisioned:
		return exitcode.OK
	case engine.StatusInvalid:
		return exitcode.InvalidManifest
	case engine.StatusTaskFailed:
		return exitcode.TaskFailed
	}
	return exitcode.ProvisioningError
}

func cmdDestroy(args []string, stdout, stderr io.Writer, d deps) int {
	file, asJSON, stateDir, positional, ok := lifecycleFlags("destroy", args, stderr)
	if !ok {
		return exitcode.Usage
	}
	if file != "" || len(positional) != 1 {
		fmt.Fprintln(stderr, "simctl destroy: usage: simctl destroy <name> [--json] [--state-dir <dir>]")
		return exitcode.Usage
	}

	sink := renderer(stdout, stderr, asJSON)
	opts, err := d.options(stateDir, sink)
	if err != nil {
		fmt.Fprintf(stderr, "simctl destroy: %v\n", err)
		return exitcode.Usage
	}

	report, err := engine.Destroy(context.Background(), positional[0], opts)
	if err != nil {
		fmt.Fprintf(stderr, "simctl destroy: %v\n", err)
		return exitcode.Usage
	}

	if asJSON {
		writeReportLine(stdout, "destroy.report", report)
	} else {
		renderDestroyReport(stdout, report)
	}

	if report.Status != engine.StatusDestroyed {
		return exitcode.ProvisioningError
	}
	return exitcode.OK
}

// renderer returns the sink apply and destroy stream into: JSON lines, or
// human-readable text. Both render the same events; there is no other path.
func renderer(stdout, stderr io.Writer, asJSON bool) events.Sink {
	if asJSON {
		return events.JSONLines(stdout, func(err error) { fmt.Fprintf(stderr, "simctl: %v\n", err) })
	}
	h := &human{w: stdout, output: map[string]*strings.Builder{}, position: map[string]string{}}
	return h.emit
}

// writeReportLine prints a report as the final JSON line, typed like an event
// so a consumer reading the stream line by line can tell it apart.
func writeReportLine(w io.Writer, typ string, report any) {
	body, err := json.Marshal(report)
	if err != nil {
		fmt.Fprintf(w, `{"type":%q,"error":%q}`+"\n", typ, err.Error())
		return
	}
	fmt.Fprintf(w, `{"type":%q,%s`+"\n", typ, body[1:])
}

// human renders events as terminal text. A Task's output is held back and
// shown only if the Task does not succeed, so a healthy build stays short.
type human struct {
	w        io.Writer
	output   map[string]*strings.Builder
	position map[string]string
}

func (h *human) emit(e events.Event) {
	switch e := e.(type) {
	case events.ApplyStarted:
		fmt.Fprintf(h.w, "Applying %s (plan %s): %s, %s\n", e.Environment, shortHash(e.PlanHash),
			count(e.Machines, "machine", "machines"), count(e.Tasks, "task", "tasks"))
	case events.NetworkCreated:
		fmt.Fprintf(h.w, "  network  %-14s %s\n", e.Network, e.Subnet)
	case events.MachineCreated:
		fmt.Fprintf(h.w, "  machine  %-14s created from %s\n", e.Machine, e.Image)
	case events.MachineReady:
		fmt.Fprintf(h.w, "  machine  %-14s ready at %s\n", e.Machine, e.IP)
	case events.TaskStarted:
		h.position[e.Task] = fmt.Sprintf("%d/%d", e.Index, e.Total)
		h.output[e.Task] = &strings.Builder{}
	case events.TaskOutput:
		if b := h.output[e.Task]; b != nil {
			b.WriteString(e.Data)
		}
	case events.TaskFinished:
		verdict := "ok"
		if e.Status != events.StatusSucceeded {
			verdict = strings.ToUpper(e.Status)
		}
		fmt.Fprintf(h.w, "  task %5s  %s on %s  %s  %s\n", h.position[e.Task], e.Task, e.Machine, verdict, e.Duration)
		if e.Status != events.StatusSucceeded {
			if e.Reason != "" {
				fmt.Fprintf(h.w, "             %s\n", e.Reason)
			}
			if out := strings.TrimRight(h.output[e.Task].String(), "\n"); out != "" {
				for _, line := range strings.Split(out, "\n") {
					fmt.Fprintf(h.w, "             | %s\n", line)
				}
			}
		}
		delete(h.output, e.Task)
	case events.DestroyStarted:
		fmt.Fprintf(h.w, "Destroying %s\n", e.Environment)
	case events.ResourceRemoved:
		fmt.Fprintf(h.w, "  removed  %-8s %s\n", e.Kind, e.Name)
	case events.Warning:
		fmt.Fprintf(h.w, "  warning: %s\n", e.Message)
	case events.Error:
		fmt.Fprintf(h.w, "  error: %s\n", e.Message)
	}
}

func renderApplyReport(w io.Writer, r engine.ApplyReport) {
	switch r.Status {
	case engine.StatusInvalid:
		renderProblems(w, r.Manifest, r.Errors)
	case engine.StatusProvisioned:
		fmt.Fprintf(w, "Environment %s provisioned.\n", r.Environment)
	default:
		fmt.Fprintf(w, "Environment %s was not provisioned: %s\n", r.Environment, r.Error)
		fmt.Fprintf(w, "What was built is left in place. Remove it with: simctl destroy %s\n", r.Environment)
	}
	if r.RunLog != "" {
		fmt.Fprintf(w, "Run log: %s\n", r.RunLog)
	}
}

func renderDestroyReport(w io.Writer, r engine.DestroyReport) {
	switch {
	case r.Status != engine.StatusDestroyed:
		fmt.Fprintf(w, "Environment %s was not fully removed: %s\n", r.Environment, r.Error)
	case len(r.Removed) == 0:
		fmt.Fprintf(w, "Nothing to destroy: %s does not exist.\n", r.Environment)
	default:
		fmt.Fprintf(w, "Environment %s destroyed.\n", r.Environment)
	}
}
