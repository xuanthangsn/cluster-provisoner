// Command simctl drives the cluster provisioning engine from a terminal.
//
// It is a thin adapter over the engine library: it parses arguments, renders
// reports and maps outcomes to exit codes, and makes no decisions of its own
// (FR-29).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"sim.dev/engine"
	"sim.dev/engine/clock"
	"sim.dev/engine/internal/exitcode"
	"sim.dev/engine/planner"
	"sim.dev/engine/provider"
	"sim.dev/engine/provider/container"
	"sim.dev/engine/spec"
	"sim.dev/engine/state"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// deps are simctl's connections to the outside world. main uses the real
// ones; tests substitute the in-memory provider and a fake clock.
type deps struct {
	provider func() (provider.Provider, error)
	stateDir func() (string, error)
	clock    clock.Clock
}

func realDeps() deps {
	return deps{
		provider: func() (provider.Provider, error) { return container.New(container.Options{}) },
		stateDir: state.DefaultRoot,
		clock:    clock.Real{},
	}
}

// run is main's body, with its streams and its exit code as values so that the
// command's observable behaviour can be tested in-process.
func run(args []string, stdout, stderr io.Writer) int {
	return runWith(args, stdout, stderr, realDeps())
}

func runWith(args []string, stdout, stderr io.Writer, d deps) int {
	if len(args) == 0 {
		usage(stderr)
		return exitcode.Usage
	}

	switch args[0] {
	case "validate":
		return cmdValidate(args[1:], stdout, stderr)
	case "plan":
		return cmdPlan(args[1:], stdout, stderr)
	case "apply":
		return cmdApply(args[1:], stdout, stderr, d)
	case "destroy":
		return cmdDestroy(args[1:], stdout, stderr, d)
	case "help", "-h", "--help":
		usage(stdout)
		return exitcode.OK
	default:
		fmt.Fprintf(stderr, "simctl: unknown command %q\n\n", args[0])
		usage(stderr)
		return exitcode.Usage
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `simctl builds and verifies Environments from a manifest.

Usage:
  simctl validate -f <manifest> [--json]   report every problem in a manifest
  simctl plan     -f <manifest> [--json]   show what apply would do, changing nothing
  simctl apply    -f <manifest> [--json]   build the Environment
  simctl destroy  <name>        [--json]   remove an Environment and everything it holds

apply and destroy also take --state-dir <dir>; the default is
$XDG_STATE_HOME/simctl, or ~/.local/state/simctl.

With --json, apply and destroy print one JSON object per line: each progress
event as it happens, then the final report, typed apply.report or
destroy.report.

Exit codes:
  0  ok
  1  simctl was invoked wrongly, or the manifest file could not be read
  2  the manifest is invalid
  4  the backend failed: the Environment could not be built or removed
  5  a Task ran and did not succeed

check and status are not implemented yet.
`)
}

// flags parses the flags both commands share.
func flags(name string, args []string, stderr io.Writer) (file string, asJSON bool, code int, ok bool) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	f := fs.String("f", "", "path to the Environment manifest")
	j := fs.Bool("json", false, "print the report as JSON")

	if err := fs.Parse(args); err != nil {
		return "", false, exitcode.Usage, false
	}
	if *f == "" {
		fmt.Fprintf(stderr, "simctl %s: -f <manifest> is required\n", name)
		return "", false, exitcode.Usage, false
	}
	return *f, *j, exitcode.OK, true
}

func cmdValidate(args []string, stdout, stderr io.Writer) int {
	file, asJSON, code, ok := flags("validate", args, stderr)
	if !ok {
		return code
	}

	report, err := engine.Validate(file)
	if err != nil {
		fmt.Fprintf(stderr, "simctl validate: %v\n", err)
		return exitcode.Usage
	}

	if asJSON {
		if err := writeJSON(stdout, report); err != nil {
			fmt.Fprintf(stderr, "simctl validate: %v\n", err)
			return exitcode.Usage
		}
	} else {
		renderValidate(stdout, report)
	}

	if !report.Valid {
		return exitcode.InvalidManifest
	}
	return exitcode.OK
}

func cmdPlan(args []string, stdout, stderr io.Writer) int {
	file, asJSON, code, ok := flags("plan", args, stderr)
	if !ok {
		return code
	}

	report, err := engine.Plan(file, planner.Passthrough{})
	if err != nil {
		fmt.Fprintf(stderr, "simctl plan: %v\n", err)
		return exitcode.Usage
	}

	if asJSON {
		if err := writeJSON(stdout, report); err != nil {
			fmt.Fprintf(stderr, "simctl plan: %v\n", err)
			return exitcode.Usage
		}
	} else {
		renderPlan(stdout, report)
	}

	if !report.Valid {
		return exitcode.InvalidManifest
	}
	return exitcode.OK
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func renderValidate(w io.Writer, r engine.ValidateReport) {
	if !r.Valid {
		renderProblems(w, r.Manifest, r.Errors)
		return
	}
	name := "(unnamed)"
	if r.Environment != nil && r.Environment.Name != "" {
		name = r.Environment.Name
	}
	fmt.Fprintf(w, "%s: %s is valid — %s, %s, %s\n",
		r.Manifest, name,
		count(r.Summary.Machines, "machine", "machines"),
		count(r.Summary.Tasks, "task", "tasks"),
		count(r.Summary.Checks, "check", "checks"),
	)
}

func renderPlan(w io.Writer, r engine.PlanReport) {
	if !r.Valid {
		renderProblems(w, r.Manifest, r.Errors)
		return
	}
	p := r.Plan

	fmt.Fprintf(w, "Plan for %s  (hash %s)\n", p.Environment, shortHash(p.Hash))
	fmt.Fprintf(w, "  backend  %s\n", p.Backend)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprint(tw, "\n  networks\n")
	for _, n := range p.Networks {
		fmt.Fprintf(tw, "    %s\t%s\n", n.Name, n.Subnet)
	}
	fmt.Fprint(tw, "\n  machines\n")
	for _, m := range p.Machines {
		fmt.Fprintf(tw, "    %s\t%s\t%s\ton %s\n", m.Name, m.IP, m.Image, m.Network)
	}
	fmt.Fprint(tw, "\n  tasks\n")
	for i, t := range p.Tasks {
		fmt.Fprintf(tw, "    %d\t%s\t%s\ton %s\n", i+1, t.Name, t.Type, t.Machine)
	}
	tw.Flush()

	fmt.Fprint(w, "\nNothing has been created. Run apply to build it.\n")
}

func renderProblems(w io.Writer, manifest string, errs spec.Errors) {
	fmt.Fprintf(w, "%s: %s\n\n", manifest, count(len(errs), "problem", "problems"))
	for _, e := range errs {
		if e.Path != "" {
			fmt.Fprintf(w, "  %s\n    %s\n", e.Path, e.Rule)
		} else {
			fmt.Fprintf(w, "  %s\n", e.Rule)
		}
		if e.Hint != "" {
			fmt.Fprintf(w, "    hint: %s\n", e.Hint)
		}
		fmt.Fprintln(w)
	}
}

func count(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
