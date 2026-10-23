//go:build docker

package e2e_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"sim.dev/engine"
	"sim.dev/engine/provider/container"
)

// The planner allocates every Environment the same default subnet until
// issue 10, so these tests build one Environment at a time.

func docker(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	cmd := exec.Command("docker", args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		return out.String(), errb.String(), ee.ExitCode()
	}
	if err != nil {
		t.Fatalf("docker %v: %v", args, err)
	}
	return out.String(), errb.String(), 0
}

func mustDocker(t *testing.T, args ...string) string {
	t.Helper()
	out, errOut, code := docker(t, args...)
	if code != 0 {
		t.Fatalf("docker %v exited %d: %s", args, code, errOut)
	}
	return out
}

// labelled lists the IDs of one kind of resource carrying env's label.
func labelled(t *testing.T, env string) (containers, networks, volumes []string) {
	t.Helper()
	f := "label=" + container.EnvironmentLabel() + "=" + env
	return strings.Fields(mustDocker(t, "ps", "-aq", "--no-trunc", "--filter", f)),
		strings.Fields(mustDocker(t, "network", "ls", "-q", "--no-trunc", "--filter", f)),
		strings.Fields(mustDocker(t, "volume", "ls", "-q", "--filter", f))
}

type fixture struct {
	env  string
	dir  string
	opts engine.Options
}

// newFixture writes manifest with its name replaced by a fresh Environment
// name. Cleanup removes, by ID, only what carries that name's label.
func newFixture(t *testing.T, manifest string) *fixture {
	t.Helper()
	if _, _, code := docker(t, "info"); code != 0 {
		t.Skip("Docker is not reachable")
	}

	var b [4]byte
	_, _ = rand.Read(b[:])
	env := "e2e-" + hex.EncodeToString(b[:])

	dir := t.TempDir()
	path := filepath.Join(dir, "environment.yaml")
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(manifest, "NAME", env)), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := container.New(container.Options{})
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{env: env, dir: path, opts: engine.Options{Provider: p, StateDir: filepath.Join(dir, "state")}}

	t.Cleanup(func() {
		cs, ns, _ := labelled(t, env)
		if len(cs) > 0 {
			docker(t, append([]string{"rm", "-fv"}, cs...)...)
		}
		if len(ns) > 0 {
			docker(t, append([]string{"network", "rm"}, ns...)...)
		}
	})
	return f
}

func (f *fixture) apply(t *testing.T) engine.ApplyReport {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	report, err := engine.Apply(ctx, f.dir, f.opts)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	return report
}

func (f *fixture) destroy(t *testing.T) engine.DestroyReport {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	report, err := engine.Destroy(ctx, f.env, f.opts)
	if err != nil {
		t.Fatalf("destroy: %v", err)
	}
	return report
}

func (f *fixture) container(machine string) string { return container.ContainerName(f.env, machine) }

const twoMachines = `
apiVersion: sim.dev/v1alpha2
kind: Environment
name: NAME
machines:
  - { name: node01, image: ubuntu:24.04 }
  - { name: node02, image: ubuntu:24.04 }
tasks:
  mark: { machine: node01, run: "hostname > /tmp/ran-on" }
`

func TestATaskRunsOnTheMachineItNamesAndNowhereElse(t *testing.T) {
	f := newFixture(t, twoMachines)
	if report := f.apply(t); report.Status != engine.StatusProvisioned {
		t.Fatalf("status %s: %s", report.Status, report.Error)
	}

	if got := mustDocker(t, "exec", f.container("node01"), "cat", "/tmp/ran-on"); got != "node01\n" {
		t.Errorf("node01 recorded %q, want %q", got, "node01\n")
	}
	if _, _, code := docker(t, "exec", f.container("node02"), "test", "-e", "/tmp/ran-on"); code == 0 {
		t.Error("the Task's side effect appeared on node02, which it did not name")
	}
}

func TestTasksRunInPlanOrderAsTheFilesystemRecords(t *testing.T) {
	// Declared out of order; needs force first, second, third.
	f := newFixture(t, `
apiVersion: sim.dev/v1alpha2
kind: Environment
name: NAME
machines: [{ name: node01, image: ubuntu:24.04 }]
tasks:
  third:  { machine: node01, run: "echo third >> /tmp/order",  needs: [second] }
  first:  { machine: node01, run: "echo first >> /tmp/order" }
  second: { machine: node01, run: "echo second >> /tmp/order", needs: [first] }
`)
	if report := f.apply(t); report.Status != engine.StatusProvisioned {
		t.Fatalf("status %s: %s", report.Status, report.Error)
	}

	if got := mustDocker(t, "exec", f.container("node01"), "cat", "/tmp/order"); got != "first\nsecond\nthird\n" {
		t.Errorf("the filesystem recorded %q, want first, second, third", got)
	}
}

func TestAFailedTaskIsReportedAndItsMachineKeptForInspection(t *testing.T) {
	f := newFixture(t, `
apiVersion: sim.dev/v1alpha2
kind: Environment
name: NAME
machines: [{ name: node01, image: ubuntu:24.04 }]
tasks:
  breaks: { machine: node01, run: "echo broken > /tmp/evidence; exit 3" }
`)
	report := f.apply(t)
	if report.Status != engine.StatusTaskFailed {
		t.Fatalf("status %s, want %s", report.Status, engine.StatusTaskFailed)
	}
	if got := mustDocker(t, "exec", f.container("node01"), "cat", "/tmp/evidence"); got != "broken\n" {
		t.Errorf("evidence = %q", got)
	}
}

// snapshot is every container, network and volume on the host, keyed by kind
// and ID, with the Environment label each carried when the snapshot was taken.
// Recording the label at that moment is what lets a resource that later
// disappears still be attributed.
type snapshot map[string]string

func takeSnapshot(t *testing.T) snapshot {
	t.Helper()
	label := `{{.Label "` + container.EnvironmentLabel() + `"}}`
	s := snapshot{}
	for kind, args := range map[string][]string{
		"container": {"ps", "-a", "--no-trunc", "--format", "{{.ID}} " + label},
		"network":   {"network", "ls", "--no-trunc", "--format", "{{.ID}} " + label},
		"volume":    {"volume", "ls", "--format", "{{.Name}} " + label},
	} {
		for _, line := range strings.Split(strings.TrimSpace(mustDocker(t, args...)), "\n") {
			id, env, _ := strings.Cut(line, " ")
			if id != "" {
				s[kind+" "+id] = env
			}
		}
	}
	return s
}

// minus lists what is in a but not in b, ignoring resources that belong to
// another engine Environment.
//
// Test packages run in parallel against one daemon, so another package's
// Environments come and go during this test, and they are recognisable by
// their label. Everything else still counts — above all an unlabelled leak,
// which is what a whole-host comparison exists to catch, and anything of the
// host's own that disappears.
func minus(a, b snapshot, self string) []string {
	var out []string
	for k, env := range a {
		if _, ok := b[k]; ok || (env != "" && env != self) {
			continue
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// describe makes a difference diagnosable. On a busy host an unrelated,
// unlabelled workload changing at the same moment would also show up here;
// its name and creation time tell it apart.
func describe(t *testing.T, keys []string) string {
	var b strings.Builder
	for _, k := range keys {
		kind, id, _ := strings.Cut(k, " ")
		var args []string
		switch kind {
		case "container":
			args = []string{"inspect", "-f", "{{.Name}} created={{.Created}} labels={{json .Config.Labels}}", id}
		case "network":
			args = []string{"network", "inspect", "-f", "{{.Name}} created={{.Created}} labels={{json .Labels}}", id}
		case "volume":
			args = []string{"volume", "inspect", "-f", "{{.Name}} created={{.CreatedAt}} labels={{json .Labels}}", id}
		}
		out, _, _ := docker(t, args...)
		b.WriteString("\n  " + k + ": " + strings.TrimSpace(out))
	}
	return b.String()
}

// TestDestroyLeavesTheHostExactlyAsItFoundIt compares the whole host before
// apply and after destroy, in both directions. A label-filtered query is blind
// to a leak that carries no label; and a destroy that removed anything it did
// not create would be the worst bug this engine could have on a shared host.
func TestDestroyLeavesTheHostExactlyAsItFoundIt(t *testing.T) {
	f := newFixture(t, twoMachines)
	before := takeSnapshot(t)

	if report := f.apply(t); report.Status != engine.StatusProvisioned {
		t.Fatalf("apply: %s: %s", report.Status, report.Error)
	}
	if during := minus(takeSnapshot(t), before, f.env); len(during) < 3 {
		t.Fatalf("expected the Environment to add 2 containers and a network, saw %d new resources", len(during))
	}

	if report := f.destroy(t); report.Status != engine.StatusDestroyed {
		t.Fatalf("destroy: %s: %s", report.Status, report.Error)
	}
	after := takeSnapshot(t)

	if leaked := minus(after, before, f.env); len(leaked) > 0 {
		t.Errorf("destroy left %d resources behind:%s", len(leaked), describe(t, leaked))
	}
	if lost := minus(before, after, f.env); len(lost) > 0 {
		t.Errorf("%d resources that existed before the test are gone:%v", len(lost), lost)
	}
	if cs, ns, vs := labelled(t, f.env); len(cs)+len(ns)+len(vs) > 0 {
		t.Errorf("labelled resources remain: containers %v, networks %v, volumes %v", cs, ns, vs)
	}
}

func TestDestroyTwiceSucceedsAndLeavesNothing(t *testing.T) {
	f := newFixture(t, twoMachines)
	f.apply(t)

	for i := 1; i <= 2; i++ {
		if report := f.destroy(t); report.Status != engine.StatusDestroyed {
			t.Fatalf("destroy #%d: %s: %s", i, report.Status, report.Error)
		}
	}
	if cs, ns, vs := labelled(t, f.env); len(cs)+len(ns)+len(vs) > 0 {
		t.Errorf("labelled resources remain: containers %v, networks %v, volumes %v", cs, ns, vs)
	}
	if _, err := os.Stat(filepath.Join(f.opts.StateDir, f.env)); !os.IsNotExist(err) {
		t.Errorf("state directory survived destroy: %v", err)
	}
}

// TestMinusAttributesResourcesExactly checks the comparison's own logic with
// synthetic snapshots, so that the "something of the host's disappeared"
// direction is exercised without risking any real resource.
func TestMinusAttributesResourcesExactly(t *testing.T) {
	before := snapshot{
		"container host-db":     "",         // the host's own workload
		"network host-net":      "",         // the host's own network
		"container other-test":  "conf-123", // another package's Environment
		"volume host-anonymous": "",         // an unlabelled volume
	}
	after := snapshot{
		"network host-net":        "",
		"volume host-anonymous":   "",
		"container mine":          "e2e-self", // this test's own, left behind
		"network unlabelled-leak": "",         // a leak carrying no label
		"container other-test-2":  "conf-456", // another package's, arriving
	}

	leaked := strings.Join(minus(after, before, "e2e-self"), ",")
	if leaked != "container mine,network unlabelled-leak" {
		t.Errorf("leaked = %s; want this test's own resource and the unlabelled one, not another test's", leaked)
	}
	lost := strings.Join(minus(before, after, "e2e-self"), ",")
	if lost != "container host-db" {
		t.Errorf("lost = %s; want the host's container, and not the other test's", lost)
	}
}
