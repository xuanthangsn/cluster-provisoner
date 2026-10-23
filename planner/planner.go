// Package planner turns a validated Environment into a Plan: the fully
// resolved, side-effect-free description of what apply will do.
//
// Planning is pure and deterministic (FR-7). The same Environment and the same
// resolved image catalog always produce the same Plan, byte for byte. Nothing
// here touches Docker, a registry, the filesystem or the clock — image
// resolution happens before planning, behind the Catalog interface, which is
// what lets this stay true while images are still pinned by digest.
package planner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"

	"sim.dev/engine/spec"
)

// SchemaVersion is the version of the Plan's own shape. It is deliberately not
// part of the Plan hash: bumping it must not make every existing Environment
// look changed and so demand a rebuild.
const SchemaVersion = "1"

// Network defaults for this release.
//
// One auto-allocated network is all the manifest can express so far. Declared
// networks, several of them, overlapping-subnet rejection and static IPs are
// issue 10, which replaces this allocation.
const (
	DefaultNetworkName = "default"
	DefaultSubnet      = "172.30.0.0/24"

	// firstHostOffset leaves the network address and a gateway free.
	firstHostOffset = 2
)

// Catalog resolves an image reference as written in the manifest into the
// reference a backend will pull.
//
// Resolution is separated from planning on purpose: it is the only part that
// needs a registry, so keeping it behind this interface is what makes the
// planner pure, deterministic and testable with no network. Issue 13 adds the
// implementation that pins by digest.
type Catalog interface {
	Resolve(ref string) (string, error)
}

// Passthrough resolves a reference to itself. It is the catalog this release
// plans with, and it is what planner tests use so that a Plan depends on
// nothing outside the manifest.
type Passthrough struct{}

// Resolve returns ref unchanged.
func (Passthrough) Resolve(ref string) (string, error) { return ref, nil }

// Plan is what apply will do, resolved down to names, addresses and order.
//
// Checks are deliberately absent. They are not part of what gets built, so
// editing a Check cannot change the Plan or its hash, and so can never force a
// rebuild (FR-25).
type Plan struct {
	SchemaVersion string    `json:"schemaVersion"`
	Environment   string    `json:"environment"`
	Backend       string    `json:"backend"`
	Networks      []Network `json:"networks"`
	Machines      []Machine `json:"machines"`
	Tasks         []Task    `json:"tasks"`

	// Hash identifies this build. Two Environments with the same hash are the
	// same build; a different hash is what makes apply refuse to overwrite.
	Hash string `json:"hash"`
}

// Network is one network the Plan will create.
type Network struct {
	Name   string `json:"name"`
	Subnet string `json:"subnet"`
}

// Machine is one Machine the Plan will create, with its image resolved and its
// address allocated.
type Machine struct {
	Name    string `json:"name"`
	Image   string `json:"image"`
	Network string `json:"network"`
	IP      string `json:"ip"`
}

// Task is one step, in the position the Plan fixes for it.
type Task struct {
	Name     string      `json:"name"`
	Type     string      `json:"type"`
	Machine  string      `json:"machine"`
	Run      string      `json:"run"`
	ExitCode int         `json:"exitCode"`
	Stdout   *spec.Match `json:"stdout,omitempty"`
	Timeout  string      `json:"timeout"`
	Attempts int         `json:"attempts"`
	Needs    []string    `json:"needs,omitempty"`
}

// New plans env.
//
// It assumes env has already been validated: New reports only the problems
// that are planning's own to find, such as an image the catalog cannot
// resolve, and reports them in the same shape as validation so that a caller
// renders both the same way.
func New(env *spec.Environment, cat Catalog) (*Plan, spec.Errors) {
	if env == nil {
		return nil, spec.Errors{{
			Rule: "nothing to plan",
			Hint: "validate a manifest first",
		}}
	}

	var errs spec.Errors

	subnet, err := netip.ParsePrefix(DefaultSubnet)
	if err != nil {
		// A malformed constant is a bug in this package, not in the manifest.
		panic("planner: default subnet is not a prefix: " + err.Error())
	}

	plan := &Plan{
		SchemaVersion: SchemaVersion,
		Environment:   env.Name,
		Backend:       env.Backend,
		Networks: []Network{{
			Name:   DefaultNetworkName,
			Subnet: DefaultSubnet,
		}},
		Machines: make([]Machine, 0, len(env.Machines)),
		Tasks:    make([]Task, 0, env.Tasks.Len()),
	}

	for i, m := range env.Machines {
		image, err := cat.Resolve(m.Image)
		if err != nil {
			errs = append(errs, spec.Error{
				Path: fmt.Sprintf("machines[%d].image", i),
				Rule: fmt.Sprintf("cannot resolve image %q: %v", m.Image, err),
				Hint: "use a known catalog alias or a full image reference",
			})
			continue
		}

		ip, ok := hostAddr(subnet, firstHostOffset+i)
		if !ok {
			errs = append(errs, spec.Error{
				Path: fmt.Sprintf("machines[%d]", i),
				Rule: fmt.Sprintf("no address left in %s for this Machine", DefaultSubnet),
				Hint: "declare fewer Machines in one Environment",
			})
			continue
		}

		plan.Machines = append(plan.Machines, Machine{
			Name:    m.Name,
			Image:   image,
			Network: DefaultNetworkName,
			IP:      ip.String(),
		})
	}

	order, err := orderTasks(env.Tasks)
	if err != nil {
		errs = append(errs, spec.Error{
			Path: "tasks",
			Rule: err.Error(),
			Hint: "every Task must be reachable in dependency order",
		})
	}
	for _, name := range order {
		t, _ := env.Tasks.Get(name)
		plan.Tasks = append(plan.Tasks, Task{
			Name:     name,
			Type:     t.Type,
			Machine:  t.Machine,
			Run:      t.Run,
			ExitCode: t.ExitCode,
			Stdout:   t.Stdout,
			Timeout:  t.Timeout.String(),
			Attempts: t.Attempts,
			Needs:    t.Needs,
		})
	}

	if len(errs) > 0 {
		return nil, errs.Sorted()
	}

	plan.Hash = plan.computeHash()
	return plan, nil
}

// orderTasks returns the Task names in the order they will run.
//
// The rule is the manifest design's: the next Task is the first in declaration
// order whose dependencies have all finished. Rescanning from the start after
// each pick is what makes that literal, and makes the order a property of the
// Plan rather than of the run.
func orderTasks(tasks spec.Section[spec.Task]) ([]string, error) {
	done := make(map[string]bool, tasks.Len())
	order := make([]string, 0, tasks.Len())

	for len(order) < tasks.Len() {
		picked := false
		for _, name := range tasks.Names() {
			if done[name] {
				continue
			}
			t, _ := tasks.Get(name)
			ready := true
			for _, dep := range t.Needs {
				if !done[dep] {
					ready = false
					break
				}
			}
			if !ready {
				continue
			}
			order = append(order, name)
			done[name] = true
			picked = true
			break
		}
		if !picked {
			return nil, fmt.Errorf("Tasks cannot be ordered: %d of %d are unreachable, which means a dependency cycle or a dependency on an undeclared Task",
				tasks.Len()-len(order), tasks.Len())
		}
	}
	return order, nil
}

// computeHash hashes everything that defines this build.
//
// The hashed fields are listed explicitly rather than taken from the whole
// Plan, so that what identifies a build is a decision visible in the source.
// SchemaVersion and Hash are excluded; Checks are not here to exclude, because
// they are not part of a Plan at all. A test changes every field of a Plan in
// turn and asserts the hash moves with exactly the fields listed here, so a
// field added to Plan and forgotten here fails the build.
func (p *Plan) computeHash() string {
	body := struct {
		Environment string    `json:"environment"`
		Backend     string    `json:"backend"`
		Networks    []Network `json:"networks"`
		Machines    []Machine `json:"machines"`
		Tasks       []Task    `json:"tasks"`
	}{
		Environment: p.Environment,
		Backend:     p.Backend,
		Networks:    p.Networks,
		Machines:    p.Machines,
		Tasks:       p.Tasks,
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		// Every field is a string, int or slice of those; marshalling cannot fail.
		panic("planner: cannot hash plan: " + err.Error())
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// hostAddr returns the nth address in a prefix, or false when that address is
// outside the prefix or is its broadcast address.
func hostAddr(prefix netip.Prefix, n int) (netip.Addr, bool) {
	base := prefix.Addr()
	if !base.Is4() || n < 0 {
		return netip.Addr{}, false
	}

	size := uint64(1) << (32 - prefix.Bits())
	if uint64(n) >= size-1 { // the last address is the broadcast address
		return netip.Addr{}, false
	}

	b := base.As4()
	v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	v += uint32(n)
	addr := netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})

	if !prefix.Contains(addr) {
		return netip.Addr{}, false
	}
	return addr, true
}
