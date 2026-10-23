package planner_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"sim.dev/engine/planner"
	"sim.dev/engine/spec"
)

var update = flag.Bool("update", false, "rewrite golden files instead of comparing against them")

const minimal = `
apiVersion: sim.dev/v1alpha2
kind: Environment
name: skeleton
machines:
  - name: node01
    image: ubuntu:24.04
tasks:
  greet:
    machine: node01
    run: echo hello
checks:
  greeted:
    machine: node01
    run: test -f /tmp/hello
`

// parse is the planner's input: a validated Environment. A manifest that does
// not validate is not the planner's problem, so the helper insists.
func parse(t *testing.T, manifest string) *spec.Environment {
	t.Helper()
	env, errs := spec.Parse([]byte(manifest))
	if len(errs) != 0 {
		t.Fatalf("manifest does not validate: %v", errs)
	}
	return env
}

func plan(t *testing.T, manifest string) *planner.Plan {
	t.Helper()
	p, errs := planner.New(parse(t, manifest), planner.Passthrough{})
	if len(errs) != 0 {
		t.Fatalf("planning failed: %v", errs)
	}
	return p
}

func TestPlanDescribesTheEnvironment(t *testing.T) {
	p := plan(t, minimal)

	if p.Environment != "skeleton" {
		t.Errorf("environment = %q, want %q", p.Environment, "skeleton")
	}
	if p.Backend != spec.BackendContainer {
		t.Errorf("backend = %q, want %q", p.Backend, spec.BackendContainer)
	}
	if p.SchemaVersion != planner.SchemaVersion {
		t.Errorf("schemaVersion = %q, want %q", p.SchemaVersion, planner.SchemaVersion)
	}
	if p.Hash == "" {
		t.Error("plan carries no hash")
	}

	if len(p.Machines) != 1 {
		t.Fatalf("machines = %d, want 1", len(p.Machines))
	}
	m := p.Machines[0]
	if m.Name != "node01" {
		t.Errorf("machine name = %q, want node01", m.Name)
	}
	if m.IP == "" {
		t.Error("machine has no allocated address")
	}
	if m.Network != planner.DefaultNetworkName {
		t.Errorf("machine network = %q, want %q", m.Network, planner.DefaultNetworkName)
	}

	if len(p.Tasks) != 1 || p.Tasks[0].Name != "greet" {
		t.Errorf("tasks = %+v, want one named greet", p.Tasks)
	}
	if got, want := p.Tasks[0].Timeout, "1m0s"; got != want {
		t.Errorf("task timeout = %q, want %q", got, want)
	}
}

func TestPlanIsDeterministic(t *testing.T) {
	first := plan(t, minimal)
	encodedFirst, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 20; i++ {
		again := plan(t, minimal)
		encodedAgain, err := json.Marshal(again)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(encodedFirst, encodedAgain) {
			t.Fatalf("plan changed between runs:\n first: %s\n again: %s", encodedFirst, encodedAgain)
		}
		if again.Hash != first.Hash {
			t.Fatalf("hash changed between runs: %q then %q", first.Hash, again.Hash)
		}
	}
}

// TestPlanGolden pins the whole Plan. It is the test that makes any change in
// planning visible in review.
func TestPlanGolden(t *testing.T) {
	p := plan(t, minimal)

	got, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')

	golden := filepath.Join("testdata", "minimal.plan.json")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("rewrote %s", golden)
		return
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v\n\nrun: go test ./planner -update", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("plan does not match %s\n got:\n%s\nwant:\n%s\n\nif the change is intended, run: go test ./planner -update",
			golden, got, want)
	}
}

// TestEditingAChecksDoesNotChangeTheHash is the property that lets an author
// refine verification against an Environment that is already built (FR-25).
func TestEditingAChecksDoesNotChangeTheHash(t *testing.T) {
	edited := strings.Replace(minimal,
		"run: test -f /tmp/hello",
		"run: test -f /tmp/something-else", 1)
	if edited == minimal {
		t.Fatal("the test's own fixture did not change")
	}

	before := plan(t, minimal)
	after := plan(t, edited)

	if before.Hash != after.Hash {
		t.Errorf("editing a Check changed the plan hash: %q then %q", before.Hash, after.Hash)
	}
}

func TestEditingATaskChangesTheHash(t *testing.T) {
	edited := strings.Replace(minimal, "run: echo hello", "run: echo goodbye", 1)
	if edited == minimal {
		t.Fatal("the test's own fixture did not change")
	}

	if before, after := plan(t, minimal), plan(t, edited); before.Hash == after.Hash {
		t.Errorf("editing a Task left the plan hash unchanged: %q", before.Hash)
	}
}

// The remaining fields that define a build are covered behaviourally here;
// hash_internal_test.go holds the structural guard that no Plan field is
// forgotten in computeHash.

func TestRenamingTheEnvironmentChangesTheHash(t *testing.T) {
	edited := strings.Replace(minimal, "name: skeleton", "name: skeleton-2", 1)
	if before, after := plan(t, minimal), plan(t, edited); before.Hash == after.Hash {
		t.Error("renaming the Environment left the plan hash unchanged")
	}
}

func TestChangingAnImageChangesTheHash(t *testing.T) {
	edited := strings.Replace(minimal, "image: ubuntu:24.04", "image: ubuntu:22.04", 1)
	if before, after := plan(t, minimal), plan(t, edited); before.Hash == after.Hash {
		t.Error("changing an image left the plan hash unchanged")
	}
}

func TestAddingAMachineChangesTheHash(t *testing.T) {
	edited := strings.Replace(minimal,
		"  - name: node01\n    image: ubuntu:24.04",
		"  - name: node01\n    image: ubuntu:24.04\n  - name: node02\n    image: ubuntu:24.04", 1)
	if edited == minimal {
		t.Fatal("the test's own fixture did not change")
	}
	if before, after := plan(t, minimal), plan(t, edited); before.Hash == after.Hash {
		t.Error("adding a Machine left the plan hash unchanged")
	}
}

func TestRenamingATaskChangesTheHash(t *testing.T) {
	edited := strings.Replace(minimal, "  greet:", "  salute:", 1)
	if before, after := plan(t, minimal), plan(t, edited); before.Hash == after.Hash {
		t.Error("renaming a Task left the plan hash unchanged")
	}
}

func TestTasksRunInTheFirstReadyDeclarationOrder(t *testing.T) {
	// c is declared first but depends on a; the documented rule is "the first
	// Task in declaration order whose dependencies have all finished", so the
	// order is a, then c, then b.
	const manifest = `
apiVersion: sim.dev/v1alpha2
kind: Environment
name: ordered
machines: [{ name: m, image: i }]
tasks:
  c: { machine: m, run: "true", needs: [a] }
  a: { machine: m, run: "true" }
  b: { machine: m, run: "true", needs: [c] }
`
	p := plan(t, manifest)

	got := make([]string, 0, len(p.Tasks))
	for _, task := range p.Tasks {
		got = append(got, task.Name)
	}
	if want := []string{"a", "c", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("task order = %v, want %v", got, want)
	}
}

func TestIndependentTasksKeepDeclarationOrder(t *testing.T) {
	const manifest = `
apiVersion: sim.dev/v1alpha2
kind: Environment
name: ordered
machines: [{ name: m, image: i }]
tasks:
  third:  { machine: m, run: "true" }
  first:  { machine: m, run: "true" }
  second: { machine: m, run: "true" }
`
	p := plan(t, manifest)

	got := make([]string, 0, len(p.Tasks))
	for _, task := range p.Tasks {
		got = append(got, task.Name)
	}
	if want := []string{"third", "first", "second"}; !reflect.DeepEqual(got, want) {
		t.Errorf("task order = %v, want %v (declaration order, not alphabetical)", got, want)
	}
}

func TestMachinesGetDistinctAddressesInDeclarationOrder(t *testing.T) {
	const manifest = `
apiVersion: sim.dev/v1alpha2
kind: Environment
name: many
machines:
  - { name: controlplane, image: i }
  - { name: node01, image: i }
  - { name: node02, image: i }
`
	p := plan(t, manifest)

	seen := map[string]string{}
	for _, m := range p.Machines {
		if other, dup := seen[m.IP]; dup {
			t.Errorf("%s and %s share the address %s", other, m.Name, m.IP)
		}
		seen[m.IP] = m.Name
	}
	if len(p.Machines) != 3 {
		t.Fatalf("machines = %d, want 3", len(p.Machines))
	}
	if p.Machines[0].IP == p.Machines[1].IP {
		t.Error("addresses are not distinct")
	}
	// Addresses come from the default subnet.
	for _, m := range p.Machines {
		if !strings.HasPrefix(m.IP, "172.30.0.") {
			t.Errorf("%s has address %s, which is outside %s", m.Name, m.IP, planner.DefaultSubnet)
		}
	}
}

func TestAddressesAreStableAcrossPlans(t *testing.T) {
	const manifest = `
apiVersion: sim.dev/v1alpha2
kind: Environment
name: stable
machines:
  - { name: a, image: i }
  - { name: b, image: i }
`
	first, again := plan(t, manifest), plan(t, manifest)

	for i := range first.Machines {
		if first.Machines[i].IP != again.Machines[i].IP {
			t.Errorf("%s moved from %s to %s between plans",
				first.Machines[i].Name, first.Machines[i].IP, again.Machines[i].IP)
		}
	}
}

func TestCatalogResolvesImages(t *testing.T) {
	p, errs := planner.New(parse(t, minimal), pinning{})
	if len(errs) != 0 {
		t.Fatalf("planning failed: %v", errs)
	}
	if got, want := p.Machines[0].Image, "ubuntu:24.04@sha256:test"; got != want {
		t.Errorf("image = %q, want %q", got, want)
	}
}

func TestUnresolvableImageIsReportedAtItsField(t *testing.T) {
	p, errs := planner.New(parse(t, minimal), failing{})
	if p != nil {
		t.Error("a plan was returned despite an unresolvable image")
	}
	if len(errs) == 0 {
		t.Fatal("expected a problem")
	}
	if errs[0].Path != "machines[0].image" {
		t.Errorf("path = %q, want machines[0].image", errs[0].Path)
	}
	if errs[0].Hint == "" {
		t.Error("error carries no hint")
	}
}

func TestPlanningNilEnvironmentIsReportedNotPanicked(t *testing.T) {
	p, errs := planner.New(nil, planner.Passthrough{})
	if p != nil {
		t.Error("expected no plan")
	}
	if len(errs) == 0 {
		t.Error("expected a problem")
	}
}

// pinning stands in for the digest-pinning catalog of issue 13.
type pinning struct{}

func (pinning) Resolve(ref string) (string, error) { return ref + "@sha256:test", nil }

type failing struct{}

func (failing) Resolve(string) (string, error) { return "", fmt.Errorf("no such alias") }
