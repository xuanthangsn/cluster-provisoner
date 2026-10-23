// Package conformance is the contract every Machine provider must meet.
//
// Run it from a backend's own tests:
//
//	func TestConformance(t *testing.T) {
//		conformance.Run(t, func(t *testing.T) conformance.Harness {
//			return conformance.Harness{Provider: myBackend, Image: "ubuntu:24.04", Subnets: free}
//		})
//	}
//
// The suite drives a provider through its interface only, so it is backend-
// neutral and runs unchanged against the in-memory fake and against Docker.
// That neutrality is also its limit: it proves a provider is consistent with
// its own account of the world, not that the account is true. A backend
// should pair it with tests that observe the real system out of band — for
// the container backend, that means asking Docker directly.
//
// The commands the suite runs are: true, hostname, cat, printf %s, id -un,
// printenv, sleep and sh -c "exit N". Harness.Image must provide them, and a
// "nobody" user.
package conformance

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"sim.dev/engine/provider"
)

// Harness is what the suite needs from a backend.
type Harness struct {
	Provider provider.Provider
	// Image is a Linux image providing the suite's commands.
	Image string
	// Subnets holds at least two non-overlapping IPv4 /24 subnets that are
	// free on the backend.
	Subnets []string
}

// Timeouts generous enough for a real backend under load.
const (
	opTimeout   = 2 * time.Minute
	execTimeout = 30 * time.Second
)

// Run runs the suite. newHarness is called once per case, so a backend may
// hand out fresh resources each time.
func Run(t *testing.T, newHarness func(t *testing.T) Harness) {
	cases := []struct {
		name string
		fn   func(*testing.T, *suite)
	}{
		{"networks can be created, listed and deleted", testNetworkLifecycle},
		{"overlapping subnets are refused", testOverlappingSubnets},
		{"a Machine needs an existing network", testMachineNeedsNetwork},
		{"a Machine moves through created, running and deleted", testMachineLifecycle},
		{"a duplicate Machine is refused", testDuplicateMachine},
		{"files are in place before the Machine first starts", testFilesBeforeStart},
		{"a Machine's hostname is its name", testHostname},
		{"exec captures exit code, stdout and stderr exactly", testExecCapture},
		{"exec streams output as well as capturing it", testExecStreams},
		{"exec honours user and environment", testExecUserEnv},
		{"exec refuses to run unbounded", testExecNeedsTimeout},
		{"exec on a missing Machine is not found", testExecMissing},
		{"files copy in and out intact", testCopyRoundTrip},
		{"background processes start and stop by name", testBackground},
		{"Environments are isolated from each other", testIsolation},
		{"an unknown Environment lists empty", testUnknownEnvironment},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			if len(h.Subnets) < 2 {
				t.Fatalf("harness must provide at least two subnets, got %d", len(h.Subnets))
			}
			s := &suite{h: h, p: h.Provider}
			tc.fn(t, s)
		})
	}
}

type suite struct {
	h Harness
	p provider.Provider
}

// env returns a fresh Environment name and removes whatever it holds when the
// test ends.
func (s *suite) env(t *testing.T) string {
	t.Helper()
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	name := "conf-" + hex.EncodeToString(b[:])
	t.Cleanup(func() { s.purge(t, name) })
	return name
}

// purge deletes everything an Environment holds, through the provider.
// Whether that really leaves nothing behind is for out-of-band tests to judge.
func (s *suite) purge(t *testing.T, env string) {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	inv, err := s.p.List(ctx, env)
	if err != nil {
		t.Errorf("cleanup: listing %s: %v", env, err)
		return
	}
	for _, m := range inv.Machines {
		if err := s.p.DeleteMachine(ctx, provider.MachineRef{Environment: env, Machine: m.Name}); err != nil {
			t.Errorf("cleanup: deleting machine %s: %v", m.Name, err)
		}
	}
	for _, n := range inv.Networks {
		if err := s.p.DeleteNetwork(ctx, provider.NetworkRef{Environment: env, Network: n}); err != nil {
			t.Errorf("cleanup: deleting network %s: %v", n, err)
		}
	}
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), opTimeout)
	t.Cleanup(cancel)
	return c
}

func (s *suite) network(t *testing.T, env, name, subnet string) {
	t.Helper()
	if err := s.p.CreateNetwork(ctx(t), provider.Network{Environment: env, Name: name, Subnet: subnet}); err != nil {
		t.Fatalf("creating network %s: %v", name, err)
	}
}

// machine creates a network and a Machine on it, with files staged.
func (s *suite) machine(t *testing.T, env, name string, files ...provider.File) provider.MachineRef {
	t.Helper()
	s.network(t, env, "net", s.h.Subnets[0])
	m := provider.Machine{
		Environment: env,
		Name:        name,
		Image:       s.h.Image,
		Network:     "net",
		IP:          hostIn(s.h.Subnets[0], 10),
		Files:       files,
	}
	if err := s.p.CreateMachine(ctx(t), m); err != nil {
		t.Fatalf("creating machine %s: %v", name, err)
	}
	return m.Ref()
}

// running creates and starts a Machine.
func (s *suite) running(t *testing.T, env, name string) provider.MachineRef {
	t.Helper()
	ref := s.machine(t, env, name)
	if err := s.p.StartMachine(ctx(t), ref); err != nil {
		t.Fatalf("starting machine %s: %v", name, err)
	}
	return ref
}

func (s *suite) exec(t *testing.T, ref provider.MachineRef, argv ...string) provider.ExecResult {
	t.Helper()
	res, err := s.p.Exec(ctx(t), provider.ExecRequest{Machine: ref, Argv: argv, Timeout: execTimeout})
	if err != nil {
		t.Fatalf("exec %q: %v", argv, err)
	}
	if res.TimedOut {
		t.Fatalf("exec %q timed out", argv)
	}
	return res
}

func testNetworkLifecycle(t *testing.T, s *suite) {
	env := s.env(t)
	s.network(t, env, "net", s.h.Subnets[0])

	inv := list(t, s.p, env)
	if strings.Join(inv.Networks, ",") != "net" {
		t.Errorf("networks = %v, want [net]", inv.Networks)
	}

	err := s.p.CreateNetwork(ctx(t), provider.Network{Environment: env, Name: "net", Subnet: s.h.Subnets[1]})
	if !errors.Is(err, provider.ErrAlreadyExists) {
		t.Errorf("creating a duplicate network: err = %v, want ErrAlreadyExists", err)
	}

	ref := provider.NetworkRef{Environment: env, Network: "net"}
	if err := s.p.DeleteNetwork(ctx(t), ref); err != nil {
		t.Fatalf("deleting network: %v", err)
	}
	if inv := list(t, s.p, env); len(inv.Networks) != 0 {
		t.Errorf("networks after delete = %v, want none", inv.Networks)
	}
	if err := s.p.DeleteNetwork(ctx(t), ref); err != nil {
		t.Errorf("deleting a deleted network must succeed: %v", err)
	}
}

func testOverlappingSubnets(t *testing.T, s *suite) {
	env := s.env(t)
	s.network(t, env, "first", s.h.Subnets[0])

	err := s.p.CreateNetwork(ctx(t), provider.Network{Environment: env, Name: "second", Subnet: s.h.Subnets[0]})
	if err == nil {
		t.Error("a network overlapping an existing subnet was accepted")
	}
}

func testMachineNeedsNetwork(t *testing.T, s *suite) {
	env := s.env(t)
	err := s.p.CreateMachine(ctx(t), provider.Machine{
		Environment: env, Name: "m", Image: s.h.Image, Network: "absent", IP: hostIn(s.h.Subnets[0], 10),
	})
	if !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func testMachineLifecycle(t *testing.T, s *suite) {
	env := s.env(t)
	ref := s.machine(t, env, "node01")

	if got := machineState(t, s.p, env, "node01"); got != provider.StateCreated {
		t.Errorf("state after create = %q, want %q", got, provider.StateCreated)
	}

	_, err := s.p.Exec(ctx(t), provider.ExecRequest{Machine: ref, Argv: []string{"true"}, Timeout: execTimeout})
	if !errors.Is(err, provider.ErrNotRunning) {
		t.Errorf("exec before start: err = %v, want ErrNotRunning", err)
	}

	if err := s.p.StartMachine(ctx(t), ref); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := machineState(t, s.p, env, "node01"); got != provider.StateRunning {
		t.Errorf("state after start = %q, want %q", got, provider.StateRunning)
	}

	// The reachability contract: once start returns, exec works at once.
	if res := s.exec(t, ref, "true"); res.ExitCode != 0 {
		t.Errorf("first exec after start exited %d", res.ExitCode)
	}

	if err := s.p.StartMachine(ctx(t), ref); err != nil {
		t.Errorf("starting a running machine must succeed: %v", err)
	}

	if err := s.p.DeleteMachine(ctx(t), ref); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if inv := list(t, s.p, env); len(inv.Machines) != 0 {
		t.Errorf("machines after delete = %v, want none", inv.Machines)
	}
	if err := s.p.DeleteMachine(ctx(t), ref); err != nil {
		t.Errorf("deleting a deleted machine must succeed: %v", err)
	}
}

func testDuplicateMachine(t *testing.T, s *suite) {
	env := s.env(t)
	s.machine(t, env, "node01")

	err := s.p.CreateMachine(ctx(t), provider.Machine{
		Environment: env, Name: "node01", Image: s.h.Image, Network: "net", IP: hostIn(s.h.Subnets[0], 11),
	})
	if !errors.Is(err, provider.ErrAlreadyExists) {
		t.Errorf("err = %v, want ErrAlreadyExists", err)
	}
}

func testFilesBeforeStart(t *testing.T, s *suite) {
	env := s.env(t)
	staged := provider.File{Path: "/etc/sim/staged.conf", Data: []byte("staged before start\n"), Mode: 0o640}
	ref := s.machine(t, env, "node01", staged)

	// Read back while the Machine has never run.
	got, err := s.p.CopyOut(ctx(t), ref, staged.Path)
	if err != nil {
		t.Fatalf("reading the staged file before start: %v", err)
	}
	if !bytes.Equal(got.Data, staged.Data) {
		t.Errorf("staged data = %q, want %q", got.Data, staged.Data)
	}
	if got.Mode.Perm() != staged.Mode {
		t.Errorf("staged mode = %v, want %v", got.Mode.Perm(), staged.Mode)
	}

	if err := s.p.StartMachine(ctx(t), ref); err != nil {
		t.Fatalf("start: %v", err)
	}
	if res := s.exec(t, ref, "cat", staged.Path); string(res.Stdout) != string(staged.Data) {
		t.Errorf("staged file read from inside = %q, want %q", res.Stdout, staged.Data)
	}
}

func testHostname(t *testing.T, s *suite) {
	env := s.env(t)
	ref := s.running(t, env, "controlplane")

	if res := s.exec(t, ref, "hostname"); string(res.Stdout) != "controlplane\n" {
		t.Errorf("hostname = %q, want %q", res.Stdout, "controlplane\n")
	}
}

func testExecCapture(t *testing.T, s *suite) {
	env := s.env(t)
	ref := s.running(t, env, "node01")

	t.Run("output without a trailing newline", func(t *testing.T) {
		res := s.exec(t, ref, "printf", "%s", "no trailing newline")
		if string(res.Stdout) != "no trailing newline" || len(res.Stderr) != 0 || res.ExitCode != 0 {
			t.Errorf("got exit=%d stdout=%q stderr=%q", res.ExitCode, res.Stdout, res.Stderr)
		}
	})
	t.Run("a non-zero exit code", func(t *testing.T) {
		if res := s.exec(t, ref, "sh", "-c", "exit 7"); res.ExitCode != 7 {
			t.Errorf("exit code = %d, want 7", res.ExitCode)
		}
	})
	t.Run("stderr apart from stdout", func(t *testing.T) {
		res := s.exec(t, ref, "cat", "/no/such/file")
		if res.ExitCode != 1 || len(res.Stdout) != 0 || len(res.Stderr) == 0 {
			t.Errorf("got exit=%d stdout=%q stderr=%q; want exit 1, empty stdout, a message on stderr",
				res.ExitCode, res.Stdout, res.Stderr)
		}
	})
	t.Run("output larger than a pipe buffer", func(t *testing.T) {
		big := strings.Repeat("0123456789", 10_000) // 100 kB, above the 64 kB pipe buffer
		if res := s.exec(t, ref, "printf", "%s", big); string(res.Stdout) != big {
			t.Errorf("captured %d bytes, want %d", len(res.Stdout), len(big))
		}
	})
}

func testExecStreams(t *testing.T, s *suite) {
	env := s.env(t)
	ref := s.running(t, env, "node01")

	var streamed bytes.Buffer
	res, err := s.p.Exec(ctx(t), provider.ExecRequest{
		Machine: ref, Argv: []string{"printf", "%s", "streamed"}, Timeout: execTimeout, Stdout: &streamed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if streamed.String() != "streamed" || string(res.Stdout) != "streamed" {
		t.Errorf("streamed %q and captured %q, want both %q", streamed.String(), res.Stdout, "streamed")
	}
}

func testExecUserEnv(t *testing.T, s *suite) {
	env := s.env(t)
	ref := s.running(t, env, "node01")

	res, err := s.p.Exec(ctx(t), provider.ExecRequest{
		Machine: ref, Argv: []string{"id", "-un"}, User: "nobody", Timeout: execTimeout,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Stdout) != "nobody\n" {
		t.Errorf("user = %q, want %q", res.Stdout, "nobody\n")
	}

	res, err = s.p.Exec(ctx(t), provider.ExecRequest{
		Machine: ref, Argv: []string{"printenv", "SIM_PROBE"}, Env: map[string]string{"SIM_PROBE": "value with spaces"},
		Timeout: execTimeout,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Stdout) != "value with spaces\n" {
		t.Errorf("env = %q, want %q", res.Stdout, "value with spaces\n")
	}
}

func testExecNeedsTimeout(t *testing.T, s *suite) {
	env := s.env(t)
	ref := s.running(t, env, "node01")

	if _, err := s.p.Exec(ctx(t), provider.ExecRequest{Machine: ref, Argv: []string{"true"}}); err == nil {
		t.Error("exec without a timeout was accepted")
	}
}

func testExecMissing(t *testing.T, s *suite) {
	env := s.env(t)
	_, err := s.p.Exec(ctx(t), provider.ExecRequest{
		Machine: provider.MachineRef{Environment: env, Machine: "ghost"}, Argv: []string{"true"}, Timeout: execTimeout,
	})
	if !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func testCopyRoundTrip(t *testing.T, s *suite) {
	env := s.env(t)
	ref := s.running(t, env, "node01")

	f := provider.File{Path: "/tmp/sim/nested/dir/secret.key", Data: []byte("key material\x00\xff"), Mode: 0o600}
	if err := s.p.CopyIn(ctx(t), ref, f); err != nil {
		t.Fatalf("copy in: %v", err)
	}

	got, err := s.p.CopyOut(ctx(t), ref, f.Path)
	if err != nil {
		t.Fatalf("copy out: %v", err)
	}
	if !bytes.Equal(got.Data, f.Data) || got.Mode.Perm() != f.Mode {
		t.Errorf("round trip gave %q %v, want %q %v", got.Data, got.Mode.Perm(), f.Data, f.Mode)
	}
	if res := s.exec(t, ref, "cat", f.Path); !bytes.Equal(res.Stdout, f.Data) {
		t.Errorf("file read from inside = %q, want %q", res.Stdout, f.Data)
	}

	if _, err := s.p.CopyOut(ctx(t), ref, "/no/such/file"); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("copying out a missing path: err = %v, want ErrNotFound", err)
	}
	ghost := provider.MachineRef{Environment: env, Machine: "ghost"}
	if err := s.p.CopyIn(ctx(t), ghost, f); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("copying into a missing machine: err = %v, want ErrNotFound", err)
	}
}

func testBackground(t *testing.T, s *suite) {
	env := s.env(t)
	created := s.machine(t, env, "idle")

	req := provider.BackgroundRequest{Machine: created, Name: "sleeper", Argv: []string{"sleep", "300"}}
	if err := s.p.StartBackground(ctx(t), req); !errors.Is(err, provider.ErrNotRunning) {
		t.Errorf("starting on a created machine: err = %v, want ErrNotRunning", err)
	}

	if err := s.p.StartMachine(ctx(t), created); err != nil {
		t.Fatal(err)
	}
	if err := s.p.StartBackground(ctx(t), req); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := s.p.StartBackground(ctx(t), req); !errors.Is(err, provider.ErrAlreadyExists) {
		t.Errorf("starting a running name again: err = %v, want ErrAlreadyExists", err)
	}
	if err := s.p.StopBackground(ctx(t), created, "sleeper"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := s.p.StopBackground(ctx(t), created, "sleeper"); err != nil {
		t.Errorf("stopping a stopped process must succeed: %v", err)
	}
	if err := s.p.StartBackground(ctx(t), req); err != nil {
		t.Errorf("a stopped name must be reusable: %v", err)
	}

	ghost := provider.MachineRef{Environment: env, Machine: "ghost"}
	if err := s.p.StopBackground(ctx(t), ghost, "sleeper"); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("stopping on a missing machine: err = %v, want ErrNotFound", err)
	}
}

func testIsolation(t *testing.T, s *suite) {
	a, b := s.env(t), s.env(t)
	for i, env := range []string{a, b} {
		s.network(t, env, "net", s.h.Subnets[i])
		err := s.p.CreateMachine(ctx(t), provider.Machine{
			Environment: env, Name: "m", Image: s.h.Image, Network: "net", IP: hostIn(s.h.Subnets[i], 10),
		})
		if err != nil {
			t.Fatalf("creating m in %s: %v", env, err)
		}
	}

	if err := s.p.DeleteMachine(ctx(t), provider.MachineRef{Environment: a, Machine: "m"}); err != nil {
		t.Fatal(err)
	}
	if inv := list(t, s.p, a); len(inv.Machines) != 0 {
		t.Errorf("%s still lists %v", a, inv.Machines)
	}
	if inv := list(t, s.p, b); len(inv.Machines) != 1 || inv.Machines[0].Name != "m" {
		t.Errorf("deleting from %s touched %s: it lists %v", a, b, inv.Machines)
	}
}

func testUnknownEnvironment(t *testing.T, s *suite) {
	if inv := list(t, s.p, s.env(t)); !inv.Empty() {
		t.Errorf("a fresh Environment lists %+v", inv)
	}
}

func list(t *testing.T, p provider.Provider, env string) provider.Inventory {
	t.Helper()
	inv, err := p.List(ctx(t), env)
	if err != nil {
		t.Fatalf("list %s: %v", env, err)
	}
	return inv
}

func machineState(t *testing.T, p provider.Provider, env, name string) provider.State {
	t.Helper()
	for _, m := range list(t, p, env).Machines {
		if m.Name == name {
			return m.State
		}
	}
	t.Fatalf("machine %s is not listed", name)
	return ""
}

// hostIn returns the nth address of a /24 written as a.b.c.0/24.
func hostIn(subnet string, n int) string {
	prefix := strings.TrimSuffix(strings.TrimSuffix(subnet, "/24"), "0")
	return prefix + strconv.Itoa(n)
}
