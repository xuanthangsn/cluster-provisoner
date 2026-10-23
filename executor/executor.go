// Package executor drives a Plan through a Machine provider: it creates the
// networks and Machines, starts them, and runs the Tasks in the Plan's order.
// It also takes an Environment apart again.
//
// It knows nothing about any particular backend. Everything it does goes
// through the provider interface, which is what lets a new backend arrive
// without changes here (NFR-9).
package executor

import (
	"context"
	"fmt"

	"sim.dev/engine/clock"
	"sim.dev/engine/events"
	"sim.dev/engine/planner"
	"sim.dev/engine/provider"
	"sim.dev/engine/taskrunner"
)

// Outcomes of an apply.
const (
	// Provisioned means every Task succeeded (FR-21).
	Provisioned = "provisioned"
	// TaskFailed means a Task's command ran and did not meet its criterion.
	TaskFailed = "task-failed"
	// ProvisioningError means the backend failed, so the build could not be
	// carried out at all.
	ProvisioningError = "provisioning-error"
)

// Executor carries out Plans.
type Executor struct {
	Provider provider.Provider
	Clock    clock.Clock
	Emit     events.Sink
}

// ApplyResult is what an apply did.
type ApplyResult struct {
	Status string
	Tasks  []taskrunner.Result
	// Error describes a provisioning error.
	Error string
}

// Apply builds plan. It stops at the first failure and leaves what it built in
// place, so the author can inspect it; destroy removes it.
func (x *Executor) Apply(ctx context.Context, plan *planner.Plan) ApplyResult {
	env := plan.Environment
	started := x.Clock.Now()
	x.emit(events.ApplyStarted{
		Time: started, Environment: env, PlanHash: plan.Hash,
		Machines: len(plan.Machines), Tasks: len(plan.Tasks),
	})

	result := x.apply(ctx, plan)

	x.emit(events.ApplyFinished{
		Time: x.Clock.Now(), Environment: env, Status: result.Status,
		Duration: x.Clock.Now().Sub(started).String(),
	})
	return result
}

func (x *Executor) apply(ctx context.Context, plan *planner.Plan) ApplyResult {
	env := plan.Environment
	fail := func(format string, args ...any) ApplyResult {
		msg := fmt.Sprintf(format, args...)
		x.emit(events.Error{Time: x.Clock.Now(), Environment: env, Message: msg})
		return ApplyResult{Status: ProvisioningError, Error: msg}
	}

	for _, n := range plan.Networks {
		err := x.Provider.CreateNetwork(ctx, provider.Network{Environment: env, Name: n.Name, Subnet: n.Subnet})
		if err != nil {
			return fail("creating network %s: %v", n.Name, err)
		}
		x.emit(events.NetworkCreated{Time: x.Clock.Now(), Environment: env, Network: n.Name, Subnet: n.Subnet})
	}

	for _, m := range plan.Machines {
		err := x.Provider.CreateMachine(ctx, provider.Machine{
			Environment: env, Name: m.Name, Image: m.Image, Network: m.Network, IP: m.IP,
		})
		if err != nil {
			return fail("creating machine %s: %v", m.Name, err)
		}
		x.emit(events.MachineCreated{Time: x.Clock.Now(), Environment: env, Machine: m.Name, Image: m.Image})
	}

	// Every Machine is created before any starts, so that later work on
	// files-before-start (issue 11) can stage files that refer to one another.
	for _, m := range plan.Machines {
		if err := x.Provider.StartMachine(ctx, provider.MachineRef{Environment: env, Machine: m.Name}); err != nil {
			return fail("starting machine %s: %v", m.Name, err)
		}
		x.emit(events.MachineReady{Time: x.Clock.Now(), Environment: env, Machine: m.Name, IP: m.IP})
	}

	runner := &taskrunner.Runner{Provider: x.Provider, Clock: x.Clock, Emit: x.Emit}
	result := ApplyResult{Status: Provisioned}
	for i, t := range plan.Tasks {
		r := runner.Run(ctx, env, t, i+1, len(plan.Tasks))
		result.Tasks = append(result.Tasks, r)
		if r.Succeeded() {
			continue
		}
		if r.Status == events.StatusError {
			result.Status = ProvisioningError
			result.Error = fmt.Sprintf("Task %s: %s", t.Name, r.Reason)
		} else {
			result.Status = TaskFailed
			result.Error = fmt.Sprintf("Task %s failed: %s", t.Name, r.Reason)
		}
		x.emit(events.Error{Time: x.Clock.Now(), Environment: env, Message: result.Error})
		break
	}
	return result
}

// Resource is one thing a destroy removed.
type Resource struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// Resource kinds.
const (
	KindMachine = "machine"
	KindNetwork = "network"
)

// Destroy removes everything the backend holds for env. It works from the
// backend's own inventory, not from any record the engine kept, and succeeds
// when there is nothing to remove (FR-27).
func (x *Executor) Destroy(ctx context.Context, env string) ([]Resource, error) {
	x.emit(events.DestroyStarted{Time: x.Clock.Now(), Environment: env})

	removed, err := x.destroy(ctx, env)
	status := events.StatusSucceeded
	if err != nil {
		status = events.StatusFailed
		x.emit(events.Error{Time: x.Clock.Now(), Environment: env, Message: err.Error()})
	}
	x.emit(events.DestroyFinished{Time: x.Clock.Now(), Environment: env, Status: status, Removed: len(removed)})
	return removed, err
}

func (x *Executor) destroy(ctx context.Context, env string) ([]Resource, error) {
	inv, err := x.Provider.List(ctx, env)
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", env, err)
	}

	var removed []Resource
	note := func(kind, name string) {
		removed = append(removed, Resource{Kind: kind, Name: name})
		x.emit(events.ResourceRemoved{Time: x.Clock.Now(), Environment: env, Kind: kind, Name: name})
	}

	// Machines first: a network cannot go while Machines are attached.
	for _, m := range inv.Machines {
		if err := x.Provider.DeleteMachine(ctx, provider.MachineRef{Environment: env, Machine: m.Name}); err != nil {
			return removed, fmt.Errorf("deleting machine %s: %w", m.Name, err)
		}
		note(KindMachine, m.Name)
	}
	for _, n := range inv.Networks {
		if err := x.Provider.DeleteNetwork(ctx, provider.NetworkRef{Environment: env, Network: n}); err != nil {
			return removed, fmt.Errorf("deleting network %s: %w", n, err)
		}
		note(KindNetwork, n)
	}

	// Ask again rather than assume. The backend's own account after deleting
	// is the only evidence this layer has that nothing is left.
	after, err := x.Provider.List(ctx, env)
	if err != nil {
		return removed, fmt.Errorf("listing %s after removal: %w", env, err)
	}
	if !after.Empty() {
		return removed, fmt.Errorf("%s still holds %d machines, %d networks and %d volumes after removal",
			env, len(after.Machines), len(after.Networks), len(after.Volumes))
	}
	return removed, nil
}

func (x *Executor) emit(e events.Event) {
	if x.Emit != nil {
		x.Emit(e)
	}
}
