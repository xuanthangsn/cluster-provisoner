package spec_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"sim.dev/engine/spec"
)

// minimal is the smallest Environment this release plans: a header, one
// Machine, one exec Task and one Check. backend is left out so that the
// default is exercised.
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

func TestParseAcceptsAMinimalManifest(t *testing.T) {
	env, errs := spec.Parse([]byte(minimal))
	if len(errs) != 0 {
		t.Fatalf("expected no problems, got: %v", errs)
	}
	if env == nil {
		t.Fatal("expected an Environment")
	}
	if env.Name != "skeleton" {
		t.Errorf("name = %q, want %q", env.Name, "skeleton")
	}
	if len(env.Machines) != 1 || env.Machines[0].Name != "node01" {
		t.Errorf("machines = %+v, want one named node01", env.Machines)
	}
	if env.Tasks.Len() != 1 || env.Checks.Len() != 1 {
		t.Errorf("tasks/checks = %d/%d, want 1/1", env.Tasks.Len(), env.Checks.Len())
	}
}

func TestParseAppliesDocumentedDefaults(t *testing.T) {
	env, errs := spec.Parse([]byte(minimal))
	if len(errs) != 0 {
		t.Fatalf("unexpected problems: %v", errs)
	}

	if env.Backend != spec.BackendContainer {
		t.Errorf("backend = %q, want %q", env.Backend, spec.BackendContainer)
	}

	task, ok := env.Tasks.Get("greet")
	if !ok {
		t.Fatal("task greet is missing")
	}
	if task.Type != spec.TaskTypeExec {
		t.Errorf("task type = %q, want %q", task.Type, spec.TaskTypeExec)
	}
	if got, want := time.Duration(task.Timeout), 60*time.Second; got != want {
		t.Errorf("task timeout = %v, want %v", got, want)
	}
	if task.Attempts != 1 {
		t.Errorf("task attempts = %d, want 1", task.Attempts)
	}
	if task.ExitCode != 0 {
		t.Errorf("task exitCode = %d, want 0", task.ExitCode)
	}

	check, ok := env.Checks.Get("greeted")
	if !ok {
		t.Fatal("check greeted is missing")
	}
	if got, want := time.Duration(check.Timeout), 60*time.Second; got != want {
		t.Errorf("check timeout = %v, want %v", got, want)
	}
}

func TestDefaultedEnvironmentIsInspectableAsJSON(t *testing.T) {
	env, _ := spec.Parse([]byte(minimal))

	encoded, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshalling the defaulted Environment: %v", err)
	}
	for _, want := range []string{`"backend":"container"`, `"type":"exec"`, `"timeout":"1m0s"`, `"attempts":1`} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("defaulted Environment does not show %s\ngot: %s", want, encoded)
		}
	}
}

func TestDeclarationOrderIsPreserved(t *testing.T) {
	const manifest = `
apiVersion: sim.dev/v1alpha2
kind: Environment
name: ordered
machines:
  - { name: m, image: i }
tasks:
  zebra:   { machine: m, run: "true" }
  apple:   { machine: m, run: "true" }
  mango:   { machine: m, run: "true" }
checks:
  zulu:    { machine: m, run: "true" }
  alpha:   { machine: m, run: "true" }
`
	env, errs := spec.Parse([]byte(manifest))
	if len(errs) != 0 {
		t.Fatalf("unexpected problems: %v", errs)
	}

	// A Go map would have lost this. Task order is derived from declaration
	// order, so losing it would silently change what the engine runs.
	if got, want := env.Tasks.Names(), []string{"zebra", "apple", "mango"}; !reflect.DeepEqual(got, want) {
		t.Errorf("task order = %v, want %v", got, want)
	}
	if got, want := env.Checks.Names(), []string{"zulu", "alpha"}; !reflect.DeepEqual(got, want) {
		t.Errorf("check order = %v, want %v", got, want)
	}
}

func TestUnknownFieldsAreReportedWithTheirPath(t *testing.T) {
	cases := []struct {
		name     string
		manifest string
		wantPath string
	}{
		{
			name: "at the top level",
			manifest: `
apiVersion: sim.dev/v1alpha2
kind: Environment
name: x
mystery: 1
machines: [{ name: m, image: i }]
`,
			wantPath: "mystery",
		},
		{
			name: "on a machine",
			manifest: `
apiVersion: sim.dev/v1alpha2
kind: Environment
name: x
machines: [{ name: m, image: i, imagee: j }]
`,
			wantPath: "machines[0].imagee",
		},
		{
			name: "on a task",
			manifest: `
apiVersion: sim.dev/v1alpha2
kind: Environment
name: x
machines: [{ name: m, image: i }]
tasks:
  t: { machine: m, run: "true", runn: "false" }
`,
			wantPath: "tasks.t.runn",
		},
		{
			name: "on a check",
			manifest: `
apiVersion: sim.dev/v1alpha2
kind: Environment
name: x
machines: [{ name: m, image: i }]
checks:
  c: { machine: m, run: "true", background: true }
`,
			wantPath: "checks.c.background",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, errs := spec.Parse([]byte(tc.manifest))
			if !hasPath(errs, tc.wantPath) {
				t.Errorf("no error at path %q; got %v", tc.wantPath, paths(errs))
			}
		})
	}
}

func TestUnknownFieldHintSuggestsTheNearMiss(t *testing.T) {
	const manifest = `
apiVersion: sim.dev/v1alpha2
kind: Environment
name: x
machines: [{ name: m, imagee: i }]
`
	_, errs := spec.Parse([]byte(manifest))

	for _, e := range errs {
		if e.Path == "machines[0].imagee" {
			if !strings.Contains(e.Hint, `"image"`) {
				t.Errorf("hint does not suggest image: %q", e.Hint)
			}
			return
		}
	}
	t.Fatalf("no error for the unknown field; got %v", paths(errs))
}

func TestUnsupportedAPIVersionNamesTheSupportedOnes(t *testing.T) {
	const manifest = `
apiVersion: sim.dev/v1beta9
kind: Environment
name: x
machines: [{ name: m, image: i }]
`
	_, errs := spec.Parse([]byte(manifest))

	for _, e := range errs {
		if e.Path == "apiVersion" {
			if !strings.Contains(e.Hint, spec.APIVersion) {
				t.Errorf("hint does not name the supported version %q: %q", spec.APIVersion, e.Hint)
			}
			return
		}
	}
	t.Fatalf("an unsupported apiVersion was accepted; errors: %v", paths(errs))
}

func TestEveryProblemIsReportedInOnePass(t *testing.T) {
	// Four independent mistakes: no apiVersion, wrong kind, no name, no machines.
	const manifest = `
kind: Environmnt
tasks: {}
`
	_, errs := spec.Parse([]byte(manifest))

	want := []string{"apiVersion", "kind", "machines", "name"}
	if got := paths(errs); !reflect.DeepEqual(got, want) {
		t.Errorf("paths = %v, want %v (all four, in one pass)", got, want)
	}
}

func TestEveryErrorCarriesAPathARuleAndAHint(t *testing.T) {
	const manifest = `
apiVersion: sim.dev/v1alpha2
kind: Environment
name: x
machines: [{ name: m }]
tasks:
  t: { machine: nope }
`
	_, errs := spec.Parse([]byte(manifest))
	if len(errs) == 0 {
		t.Fatal("expected problems")
	}

	for _, e := range errs {
		if e.Path == "" {
			t.Errorf("error without a field path: %+v", e)
		}
		if e.Rule == "" {
			t.Errorf("error without a rule: %+v", e)
		}
		if e.Hint == "" {
			t.Errorf("error without a hint: %+v", e)
		}
	}
}

func TestErrorOrderIsDeterministic(t *testing.T) {
	const manifest = `
apiVersion: sim.dev/v1beta9
kind: Nope
machines:
  - { name: m }
  - { nope: 1 }
tasks:
  b: { machine: ghost, needs: [missing] }
  a: {}
`
	first, _ := secondReturn(spec.Parse([]byte(manifest)))
	for i := 0; i < 20; i++ {
		again, _ := secondReturn(spec.Parse([]byte(manifest)))
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("order changed between runs:\n first: %v\n again: %v", first, again)
		}
	}

	sorted := make([]string, len(first))
	copy(sorted, first)
	for i := 1; i < len(sorted); i++ {
		if sorted[i-1] > sorted[i] {
			t.Errorf("paths are not in sorted order: %v", sorted)
			break
		}
	}
}

func TestValidationRules(t *testing.T) {
	cases := []struct {
		name      string
		manifest  string
		wantPaths []string
	}{
		{
			name:      "apiVersion is required",
			manifest:  "kind: Environment\nname: x\nmachines: [{name: m, image: i}]\n",
			wantPaths: []string{"apiVersion"},
		},
		{
			name:      "kind must be Environment",
			manifest:  "apiVersion: sim.dev/v1alpha2\nkind: Pod\nname: x\nmachines: [{name: m, image: i}]\n",
			wantPaths: []string{"kind"},
		},
		{
			name:      "name is required",
			manifest:  "apiVersion: sim.dev/v1alpha2\nkind: Environment\nmachines: [{name: m, image: i}]\n",
			wantPaths: []string{"name"},
		},
		{
			name:      "the vm backend is reserved",
			manifest:  "apiVersion: sim.dev/v1alpha2\nkind: Environment\nname: x\nbackend: vm\nmachines: [{name: m, image: i}]\n",
			wantPaths: []string{"backend"},
		},
		{
			name:      "an unknown backend is rejected",
			manifest:  "apiVersion: sim.dev/v1alpha2\nkind: Environment\nname: x\nbackend: podman\nmachines: [{name: m, image: i}]\n",
			wantPaths: []string{"backend"},
		},
		{
			name:      "at least one machine is required",
			manifest:  "apiVersion: sim.dev/v1alpha2\nkind: Environment\nname: x\n",
			wantPaths: []string{"machines"},
		},
		{
			name:      "a machine needs a name",
			manifest:  "apiVersion: sim.dev/v1alpha2\nkind: Environment\nname: x\nmachines: [{image: i}]\n",
			wantPaths: []string{"machines[0].name"},
		},
		{
			name:      "a machine needs an image",
			manifest:  "apiVersion: sim.dev/v1alpha2\nkind: Environment\nname: x\nmachines: [{name: m}]\n",
			wantPaths: []string{"machines[0].image"},
		},
		{
			name:      "machine names are unique",
			manifest:  "apiVersion: sim.dev/v1alpha2\nkind: Environment\nname: x\nmachines: [{name: m, image: i}, {name: m, image: i}]\n",
			wantPaths: []string{"machines[1].name"},
		},
		{
			name: "a task needs a machine",
			manifest: "apiVersion: sim.dev/v1alpha2\nkind: Environment\nname: x\nmachines: [{name: m, image: i}]\n" +
				"tasks:\n  t: { run: \"true\" }\n",
			wantPaths: []string{"tasks.t.machine"},
		},
		{
			name: "a task's machine must be declared",
			manifest: "apiVersion: sim.dev/v1alpha2\nkind: Environment\nname: x\nmachines: [{name: m, image: i}]\n" +
				"tasks:\n  t: { machine: ghost, run: \"true\" }\n",
			wantPaths: []string{"tasks.t.machine"},
		},
		{
			name: "a task needs a command",
			manifest: "apiVersion: sim.dev/v1alpha2\nkind: Environment\nname: x\nmachines: [{name: m, image: i}]\n" +
				"tasks:\n  t: { machine: m }\n",
			wantPaths: []string{"tasks.t.run"},
		},
		{
			name: "a dependency must name a declared task",
			manifest: "apiVersion: sim.dev/v1alpha2\nkind: Environment\nname: x\nmachines: [{name: m, image: i}]\n" +
				"tasks:\n  t: { machine: m, run: \"true\", needs: [ghost] }\n",
			wantPaths: []string{"tasks.t.needs[0]"},
		},
		{
			name: "a dependency cycle is rejected",
			manifest: "apiVersion: sim.dev/v1alpha2\nkind: Environment\nname: x\nmachines: [{name: m, image: i}]\n" +
				"tasks:\n  a: { machine: m, run: \"true\", needs: [b] }\n  b: { machine: m, run: \"true\", needs: [a] }\n",
			wantPaths: []string{"tasks.b.needs[0]"},
		},
		{
			name: "a task cannot depend on itself",
			manifest: "apiVersion: sim.dev/v1alpha2\nkind: Environment\nname: x\nmachines: [{name: m, image: i}]\n" +
				"tasks:\n  a: { machine: m, run: \"true\", needs: [a] }\n",
			wantPaths: []string{"tasks.a.needs[0]"},
		},
		{
			name: "a check needs a command",
			manifest: "apiVersion: sim.dev/v1alpha2\nkind: Environment\nname: x\nmachines: [{name: m, image: i}]\n" +
				"checks:\n  c: { machine: m }\n",
			wantPaths: []string{"checks.c.run"},
		},
		{
			name: "a check's machine must be declared",
			manifest: "apiVersion: sim.dev/v1alpha2\nkind: Environment\nname: x\nmachines: [{name: m, image: i}]\n" +
				"checks:\n  c: { machine: ghost, run: \"true\" }\n",
			wantPaths: []string{"checks.c.machine"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, errs := spec.Parse([]byte(tc.manifest))
			if got := paths(errs); !reflect.DeepEqual(got, tc.wantPaths) {
				t.Errorf("paths = %v, want %v\nerrors: %v", got, tc.wantPaths, errs)
			}
		})
	}
}

func TestMalformedYAMLIsReportedNotPanicked(t *testing.T) {
	for _, manifest := range []string{
		"apiVersion: [unclosed\n",
		"\tapiVersion: x\n",
		"",
		"just a string\n",
		"- a\n- b\n",
	} {
		_, errs := spec.Parse([]byte(manifest))
		if len(errs) == 0 {
			t.Errorf("accepted a manifest that is not an Environment: %q", manifest)
		}
	}
}

func TestWronglyTypedValueIsReportedWithItsPath(t *testing.T) {
	const manifest = `
apiVersion: sim.dev/v1alpha2
kind: Environment
name: x
machines: [{ name: m, image: i }]
tasks:
  t:
    machine: m
    run: "true"
    attempts: not-a-number
`
	_, errs := spec.Parse([]byte(manifest))
	if !hasPath(errs, "tasks.t.attempts") {
		t.Errorf("no error at tasks.t.attempts; got %v", paths(errs))
	}
}

func TestBadDurationIsReported(t *testing.T) {
	const manifest = `
apiVersion: sim.dev/v1alpha2
kind: Environment
name: x
machines: [{ name: m, image: i }]
tasks:
  t: { machine: m, run: "true", timeout: soon }
`
	_, errs := spec.Parse([]byte(manifest))
	if len(errs) == 0 {
		t.Fatal("accepted a timeout that is not a duration")
	}
}

// helpers

func paths(errs spec.Errors) []string {
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		out = append(out, e.Path)
	}
	return out
}

func hasPath(errs spec.Errors, path string) bool {
	for _, e := range errs {
		if e.Path == path {
			return true
		}
	}
	return false
}

// secondReturn adapts Parse's two results for the determinism test.
func secondReturn(_ *spec.Environment, errs spec.Errors) ([]string, spec.Errors) {
	return paths(errs), errs
}
