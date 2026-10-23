// Package engine is the library surface of the cluster provisioning engine.
//
// The library and simctl offer the same operations (FR-29): simctl is a thin
// adapter over this package and adds no behaviour of its own. A capability
// reachable from the command line but not from here is a bug.
//
// This release implements Validate, Plan, Apply and Destroy. Check and Status
// are issue 03.
package engine

import (
	"context"
	"errors"
	"fmt"
	"os"

	"sim.dev/engine/clock"
	"sim.dev/engine/events"
	"sim.dev/engine/executor"
	"sim.dev/engine/planner"
	"sim.dev/engine/provider"
	"sim.dev/engine/spec"
	"sim.dev/engine/state"
	"sim.dev/engine/taskrunner"
)

// ReportSchemaVersion is the version of the report shapes in this package.
//
// Within a manifest API version these shapes only gain fields, so a consumer
// that parses them keeps working across engine releases (NFR-12). Issue 18
// adds the test that holds that promise.
const ReportSchemaVersion = "1"

// Summary counts what a manifest declares, for a one-line answer to "what is
// in this file".
type Summary struct {
	Machines int `json:"machines"`
	Tasks    int `json:"tasks"`
	Checks   int `json:"checks"`
}

// ValidateReport is the result of Validate.
type ValidateReport struct {
	SchemaVersion string `json:"schemaVersion"`
	Manifest      string `json:"manifest"`
	Valid         bool   `json:"valid"`

	// Environment is the manifest with every documented default applied, so an
	// author can see what the engine actually works from (FR-2). It is present
	// whenever the manifest could be decoded, including when validation failed.
	Environment *spec.Environment `json:"environment,omitempty"`

	Summary Summary     `json:"summary"`
	Errors  spec.Errors `json:"errors,omitempty"`
}

// PlanReport is the result of Plan.
type PlanReport struct {
	SchemaVersion string `json:"schemaVersion"`
	Manifest      string `json:"manifest"`
	Valid         bool   `json:"valid"`

	// Plan is present only when the manifest validated and planned.
	Plan *planner.Plan `json:"plan,omitempty"`

	Errors spec.Errors `json:"errors,omitempty"`
}

// Validate decodes, defaults and validates the manifest at path.
//
// A manifest problem is reported in the returned report, not as an error. The
// error return is for reaching the file at all, so that a caller can tell "I
// could not read this" from "this is not a valid Environment".
//
// Validate needs no Docker and no network (FR-3).
func Validate(path string) (ValidateReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ValidateReport{}, fmt.Errorf("reading manifest: %w", err)
	}

	env, errs := spec.Parse(data)

	report := ValidateReport{
		SchemaVersion: ReportSchemaVersion,
		Manifest:      path,
		Valid:         len(errs) == 0,
		Environment:   env,
		Errors:        errs,
	}
	if env != nil {
		report.Summary = Summary{
			Machines: len(env.Machines),
			Tasks:    env.Tasks.Len(),
			Checks:   env.Checks.Len(),
		}
	}
	return report, nil
}

// Plan resolves the manifest at path into a Plan, without side effects: it
// creates nothing and writes no state (FR-7).
//
// cat resolves image references; pass planner.Passthrough{} to plan against
// the manifest alone.
func Plan(path string, cat planner.Catalog) (PlanReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PlanReport{}, fmt.Errorf("reading manifest: %w", err)
	}

	report := PlanReport{
		SchemaVersion: ReportSchemaVersion,
		Manifest:      path,
	}

	env, errs := spec.Parse(data)
	if len(errs) > 0 {
		report.Errors = errs
		return report, nil
	}

	plan, planErrs := planner.New(env, cat)
	if len(planErrs) > 0 {
		report.Errors = planErrs
		return report, nil
	}

	report.Valid = true
	report.Plan = plan
	return report, nil
}

// Statuses an apply or destroy ends in.
const (
	StatusProvisioned       = executor.Provisioned
	StatusTaskFailed        = executor.TaskFailed
	StatusProvisioningError = executor.ProvisioningError
	StatusInvalid           = "invalid"
	StatusDestroyed         = "destroyed"
)

// Options are what Apply and Destroy act through. Every dependency is
// explicit, so that the engine can be driven against the in-memory provider
// and a fake clock exactly as it is driven against Docker.
type Options struct {
	// Provider is the backend. Required.
	Provider provider.Provider
	// StateDir is the state directory root. Required.
	StateDir string
	// Clock defaults to the system clock.
	Clock clock.Clock
	// Events receives progress as it happens. Defaults to discarding it.
	Events events.Sink
	// Catalog resolves images. Defaults to planner.Passthrough.
	Catalog planner.Catalog
}

func (o Options) withDefaults() (Options, error) {
	if o.Provider == nil {
		return o, errors.New("engine: no Machine provider given")
	}
	if o.StateDir == "" {
		return o, errors.New("engine: no state directory given")
	}
	if o.Clock == nil {
		o.Clock = clock.Real{}
	}
	if o.Events == nil {
		o.Events = events.Discard
	}
	if o.Catalog == nil {
		o.Catalog = planner.Passthrough{}
	}
	return o, nil
}

// ApplyReport is the result of Apply.
type ApplyReport struct {
	SchemaVersion string `json:"schemaVersion"`
	Manifest      string `json:"manifest"`
	Environment   string `json:"environment,omitempty"`
	Status        string `json:"status"`
	PlanHash      string `json:"planHash,omitempty"`

	Tasks []taskrunner.Result `json:"tasks,omitempty"`

	// Errors lists manifest problems when Status is invalid.
	Errors spec.Errors `json:"errors,omitempty"`
	// Error describes what stopped the build otherwise.
	Error string `json:"error,omitempty"`

	// RunLog is the path of this run's full event log.
	RunLog string `json:"runLog,omitempty"`
}

// Apply builds the Environment the manifest at path describes.
//
// It is create-only: an Environment that already exists is refused rather
// than changed. (Recognising an unchanged Environment as up to date, and
// recreating on request, is issue 14.) A failed apply leaves what it built in
// place for inspection; Destroy removes it.
//
// The error return is for failing to start at all — an unreadable manifest,
// missing Options. Everything about the build itself is in the report.
func Apply(ctx context.Context, path string, opts Options) (ApplyReport, error) {
	opts, err := opts.withDefaults()
	if err != nil {
		return ApplyReport{}, err
	}

	planned, err := Plan(path, opts.Catalog)
	if err != nil {
		return ApplyReport{}, err
	}
	report := ApplyReport{SchemaVersion: ReportSchemaVersion, Manifest: path}
	if !planned.Valid {
		report.Status = StatusInvalid
		report.Errors = planned.Errors
		return report, nil
	}

	plan := planned.Plan
	env := plan.Environment
	report.Environment = env
	report.PlanHash = plan.Hash

	fail := func(format string, args ...any) (ApplyReport, error) {
		report.Status = StatusProvisioningError
		report.Error = fmt.Sprintf(format, args...)
		return report, nil
	}

	existing, err := opts.Provider.List(ctx, env)
	if err != nil {
		return fail("checking whether %s exists: %v", env, err)
	}
	if !existing.Empty() {
		return fail("Environment %s already exists; destroy it first", env)
	}

	dir := state.New(opts.StateDir)
	// The backend holds nothing, so any state left here is from an Environment
	// that no longer exists.
	if err := dir.Remove(env); err != nil {
		return fail("%v", err)
	}
	if err := dir.Create(env); err != nil {
		return fail("%v", err)
	}
	if err := dir.WritePlan(env, plan); err != nil {
		return fail("recording the plan: %v", err)
	}
	logFile, logPath, err := dir.CreateRunLog(env, opts.Clock.Now())
	if err != nil {
		return fail("%v", err)
	}
	defer logFile.Close()
	report.RunLog = logPath

	sink := events.Tee(opts.Events, events.JSONLines(logFile, func(err error) {
		opts.Events(events.Warning{Time: opts.Clock.Now(), Environment: env, Message: "run log: " + err.Error()})
	}))

	x := &executor.Executor{Provider: opts.Provider, Clock: opts.Clock, Emit: sink}
	result := x.Apply(ctx, plan)

	report.Status = result.Status
	report.Tasks = result.Tasks
	report.Error = result.Error
	return report, nil
}

// DestroyReport is the result of Destroy.
type DestroyReport struct {
	SchemaVersion string              `json:"schemaVersion"`
	Environment   string              `json:"environment"`
	Status        string              `json:"status"`
	Removed       []executor.Resource `json:"removed,omitempty"`
	Error         string              `json:"error,omitempty"`
}

// Destroy removes every Machine, network and volume of the named Environment,
// and its state directory. It succeeds when nothing exists (FR-27).
//
// It needs no manifest: what to remove comes from the backend's own
// inventory.
func Destroy(ctx context.Context, name string, opts Options) (DestroyReport, error) {
	opts, err := opts.withDefaults()
	if err != nil {
		return DestroyReport{}, err
	}
	dir := state.New(opts.StateDir)
	if _, err := dir.Path(name); err != nil {
		return DestroyReport{}, err
	}

	report := DestroyReport{SchemaVersion: ReportSchemaVersion, Environment: name}

	x := &executor.Executor{Provider: opts.Provider, Clock: opts.Clock, Emit: opts.Events}
	removed, err := x.Destroy(ctx, name)
	report.Removed = removed
	if err != nil {
		// Keep the state: it is the record of what this Environment was.
		report.Status = StatusProvisioningError
		report.Error = err.Error()
		return report, nil
	}

	if err := dir.Remove(name); err != nil {
		report.Status = StatusProvisioningError
		report.Error = err.Error()
		return report, nil
	}
	report.Status = StatusDestroyed
	return report, nil
}
