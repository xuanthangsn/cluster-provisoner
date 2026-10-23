package spec

import (
	"fmt"
	"strings"
)

// SupportedAPIVersions lists every manifest version this engine understands,
// so that a rejection can name them (FR-1).
var SupportedAPIVersions = []string{APIVersion}

// applyDefaults fills in the documented defaults, before validation, so that
// validation and planning both see one fully resolved manifest.
func (e *Environment) applyDefaults() {
	if e.Backend == "" {
		e.Backend = DefaultBackend
	}
	for _, name := range e.Tasks.Names() {
		t, _ := e.Tasks.Get(name)
		if t.Type == "" {
			t.Type = DefaultTaskType
		}
		if t.Timeout == 0 {
			t.Timeout = DefaultTimeout
		}
		if t.Attempts == 0 {
			t.Attempts = DefaultAttempts
		}
		e.Tasks.Set(name, t)
	}
	for _, name := range e.Checks.Names() {
		c, _ := e.Checks.Get(name)
		if c.Timeout == 0 {
			c.Timeout = DefaultTimeout
		}
		e.Checks.Set(name, c)
	}
}

// validate applies the rules this release enforces.
//
// Scope: these are the rules needed to decode, default and plan an
// Environment. The remaining rules from the manifest design — DNS-1123 label
// shapes and length caps, the full exec/transfer field matrix, template
// references, local file existence — and the negative corpus that pins all of
// them down are issue 04. Two rules below (a needs entry naming a declared
// Task, and the absence of cycles) belong to that set too, and are enforced
// here because the planner cannot order Tasks safely without them.
func (e *Environment) validate() Errors {
	var errs Errors

	e.validateHeader(&errs)
	machines := e.validateMachines(&errs)
	e.validateTasks(&errs, machines)
	e.validateChecks(&errs, machines)

	return errs
}

func (e *Environment) validateHeader(errs *Errors) {
	switch e.APIVersion {
	case "":
		errs.add("apiVersion", "required",
			fmt.Sprintf("set it to %q", APIVersion))
	case APIVersion:
	default:
		errs.add("apiVersion",
			fmt.Sprintf("unsupported manifest version %q", e.APIVersion),
			"this engine supports: "+strings.Join(SupportedAPIVersions, ", "))
	}

	switch e.Kind {
	case "":
		errs.add("kind", "required", fmt.Sprintf("set it to %q", Kind))
	case Kind:
	default:
		errs.add("kind", fmt.Sprintf("must be %q, got %q", Kind, e.Kind),
			"this engine describes one kind of object")
	}

	if e.Name == "" {
		errs.add("name", "required",
			"name the Environment; it prefixes every resource the engine creates")
	}

	switch e.Backend {
	case BackendContainer:
	case BackendVM:
		errs.add("backend", "the vm backend is reserved for a later release",
			"use backend: container, or drop the field to take the default")
	default:
		errs.add("backend", fmt.Sprintf("unknown backend %q", e.Backend),
			fmt.Sprintf("the only backend in this release is %q", BackendContainer))
	}
}

// validateMachines returns the set of declared Machine names, which the Task
// and Check rules resolve references against.
func (e *Environment) validateMachines(errs *Errors) map[string]bool {
	declared := make(map[string]bool, len(e.Machines))

	if len(e.Machines) == 0 {
		errs.add("machines", "at least one Machine is required",
			"declare a Machine with a name and an image")
		return declared
	}

	firstAt := make(map[string]int, len(e.Machines))
	for i, m := range e.Machines {
		path := fmt.Sprintf("machines[%d]", i)
		switch {
		case m.Name == "":
			errs.add(path+".name", "required",
				"a Machine's name becomes its hostname, so it must be given explicitly")
		default:
			if at, dup := firstAt[m.Name]; dup {
				errs.add(path+".name",
					fmt.Sprintf("duplicate Machine name %q, already declared at machines[%d]", m.Name, at),
					"Machine names must be unique: they become hostnames")
			} else {
				firstAt[m.Name] = i
				declared[m.Name] = true
			}
		}
		if m.Image == "" {
			errs.add(path+".image", "required",
				"give an image reference or a catalog alias")
		}
	}
	return declared
}

func (e *Environment) validateTasks(errs *Errors, machines map[string]bool) {
	for _, name := range e.Tasks.Names() {
		t, _ := e.Tasks.Get(name)
		path := "tasks." + name

		if t.Type != TaskTypeExec {
			errs.add(path+".type",
				fmt.Sprintf("unsupported Task type %q", t.Type),
				fmt.Sprintf("the only Task type in this release is %q", TaskTypeExec))
		}
		if t.Run == "" {
			errs.add(path+".run", "required",
				"give the Task a command to run")
		}
		e.requireMachine(errs, path+".machine", t.Machine, machines,
			"an exec Task runs on exactly one Machine")
		t.Stdout.validate(errs, path+".stdout")

		for i, dep := range t.Needs {
			if _, ok := e.Tasks.Get(dep); !ok {
				errs.add(fmt.Sprintf("%s.needs[%d]", path, i),
					fmt.Sprintf("no Task named %q is declared", dep),
					"a dependency must name a Task in this manifest")
			}
		}
	}
	e.validateTaskGraph(errs)
}

func (e *Environment) validateChecks(errs *Errors, machines map[string]bool) {
	for _, name := range e.Checks.Names() {
		c, _ := e.Checks.Get(name)
		path := "checks." + name

		if c.Run == "" {
			errs.add(path+".run", "required",
				"give the Check a command to run")
		}
		e.requireMachine(errs, path+".machine", c.Machine, machines,
			"a Check runs on exactly one Machine")
		c.Stdout.validate(errs, path+".stdout")
	}
}

// requireMachine reports a missing or unresolvable Machine reference.
func (e *Environment) requireMachine(errs *Errors, path, name string, machines map[string]bool, why string) {
	if name == "" {
		errs.add(path, "required", why)
		return
	}
	if !machines[name] {
		errs.add(path, fmt.Sprintf("no Machine named %q is declared", name),
			"name one of the Machines in the machines section")
	}
}

// validateTaskGraph reports dependency cycles, which would otherwise leave the
// planner unable to order the Tasks.
func (e *Environment) validateTaskGraph(errs *Errors) {
	const (
		unvisited = 0
		onStack   = 1
		done      = 2
	)
	state := make(map[string]int, e.Tasks.Len())
	reported := make(map[string]bool, e.Tasks.Len())
	var stack []string

	var visit func(string)
	visit = func(name string) {
		state[name] = onStack
		stack = append(stack, name)

		t, _ := e.Tasks.Get(name)
		for i, dep := range t.Needs {
			if _, ok := e.Tasks.Get(dep); !ok {
				continue // dangling, already reported
			}
			switch state[dep] {
			case unvisited:
				visit(dep)
			case onStack:
				if !reported[name] {
					reported[name] = true
					errs.add(fmt.Sprintf("tasks.%s.needs[%d]", name, i),
						"dependency cycle: "+strings.Join(cycleFrom(stack, dep), " -> "),
						"remove one dependency in this cycle so the Tasks can be ordered")
				}
			}
		}

		stack = stack[:len(stack)-1]
		state[name] = done
	}

	for _, name := range e.Tasks.Names() {
		if state[name] == unvisited {
			visit(name)
		}
	}
}

// cycleFrom renders the cycle closed by an edge back to start.
func cycleFrom(stack []string, start string) []string {
	for i, n := range stack {
		if n == start {
			return append(append([]string{}, stack[i:]...), start)
		}
	}
	return append(append([]string{}, stack...), start)
}
