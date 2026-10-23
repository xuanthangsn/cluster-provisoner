//go:build docker

package container_test

// Tier B: every assertion here is made by asking Docker directly, through the
// docker CLI, never through the provider. The conformance suite proves the
// provider is consistent with its own account; these prove the account is
// true. A fake satisfies "files are staged before start" or "the hostname is
// the Machine's name" by construction — only an outside observer can falsify
// them.

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"strings"
	"testing"
	"time"

	"sim.dev/engine/provider"
	"sim.dev/engine/provider/container"
)

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	return c
}

// envFor returns a fresh Environment name, removed via the CLI at the end.
func envFor(t *testing.T) string {
	t.Helper()
	var b [4]byte
	_, _ = rand.Read(b[:])
	env := "tierb-" + hex.EncodeToString(b[:])
	t.Cleanup(func() { purge(t, env) })
	return env
}

// machineOn creates a network and Machines through the provider.
func machinesOn(t *testing.T, p *container.Provider, env, img string, names ...string) []provider.MachineRef {
	t.Helper()
	subnet := freeSubnets(t, 1)[0]
	if err := p.CreateNetwork(ctx(t), provider.Network{Environment: env, Name: "net", Subnet: subnet}); err != nil {
		t.Fatal(err)
	}
	prefix := strings.TrimSuffix(subnet, "0/24")
	var refs []provider.MachineRef
	for i, name := range names {
		m := provider.Machine{Environment: env, Name: name, Image: img, Network: "net", IP: prefix + itoa(10+i)}
		if err := p.CreateMachine(ctx(t), m); err != nil {
			t.Fatal(err)
		}
		refs = append(refs, m.Ref())
	}
	return refs
}

func TestAMachineIsRunningUnderItsNameAsDockerSeesIt(t *testing.T) {
	p, env := newProvider(t), envFor(t)
	ref := machinesOn(t, p, env, image, "node01")[0]
	if err := p.StartMachine(ctx(t), ref); err != nil {
		t.Fatal(err)
	}
	c := container.ContainerName(env, "node01")

	got := strings.TrimSpace(mustDocker(t, "inspect", "-f", "{{.State.Status}}|{{.Config.Hostname}}", c))
	if got != "running|node01" {
		t.Errorf("docker inspect: state|hostname = %q, want running|node01", got)
	}
	// Docker's configured hostname and the kernel's can differ; FR-11 is about
	// the one a process inside actually sees.
	if kernel := strings.TrimSpace(mustDocker(t, "exec", c, "hostname")); kernel != "node01" {
		t.Errorf("kernel hostname = %q, want node01", kernel)
	}
}

func TestEveryResourceIsLabelledWithItsEnvironment(t *testing.T) {
	p, env := newProvider(t), envFor(t)
	machinesOn(t, p, env, image, "node01", "node02")
	label := "label=" + container.EnvironmentLabel() + "=" + env

	containers := strings.Fields(mustDocker(t, "ps", "-a", "--filter", label, "--format", "{{.Names}}"))
	want := []string{container.ContainerName(env, "node01"), container.ContainerName(env, "node02")}
	if strings.Join(sorted(containers), ",") != strings.Join(want, ",") {
		t.Errorf("labelled containers = %v, want exactly %v", containers, want)
	}

	networks := strings.Fields(mustDocker(t, "network", "ls", "--filter", label, "--format", "{{.Name}}"))
	if strings.Join(networks, ",") != container.NetworkName(env, "net") {
		t.Errorf("labelled networks = %v, want exactly [%s]", networks, container.NetworkName(env, "net"))
	}

	// Labels must also say which Machine each container is, for recovery.
	for _, name := range []string{"node01", "node02"} {
		got := strings.TrimSpace(mustDocker(t, "inspect", "-f", `{{index .Config.Labels "dev.sim.machine"}}`,
			container.ContainerName(env, name)))
		if got != name {
			t.Errorf("%s carries machine label %q", name, got)
		}
	}
}

func TestFilesAreInPlaceBeforeTheMachineHasEverStarted(t *testing.T) {
	p, env := newProvider(t), envFor(t)
	subnet := freeSubnets(t, 1)[0]
	if err := p.CreateNetwork(ctx(t), provider.Network{Environment: env, Name: "net", Subnet: subnet}); err != nil {
		t.Fatal(err)
	}
	staged := provider.File{Path: "/etc/sim/staged.conf", Data: []byte("staged before start\n"), Mode: 0o640}
	m := provider.Machine{Environment: env, Name: "node01", Image: image, Network: "net",
		IP: strings.TrimSuffix(subnet, "0/24") + "10", Files: []provider.File{staged}}
	if err := p.CreateMachine(ctx(t), m); err != nil {
		t.Fatal(err)
	}
	c := container.ContainerName(env, "node01")

	// The container must never have run: Docker records a zero StartedAt for
	// a container that has not started once.
	got := strings.TrimSpace(mustDocker(t, "inspect", "-f", "{{.State.Status}}|{{.State.StartedAt}}", c))
	if got != "created|0001-01-01T00:00:00Z" {
		t.Fatalf("container is not a never-started container: %q", got)
	}

	// Read the file out of the still-created container with docker cp.
	archive := mustDocker(t, "cp", c+":"+staged.Path, "-")
	tr := tar.NewReader(strings.NewReader(archive))
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("docker cp gave no archive: %v", err)
	}
	data, _ := io.ReadAll(tr)
	if string(data) != string(staged.Data) {
		t.Errorf("file read by docker cp before start = %q, want %q", data, staged.Data)
	}
	if mode := hdr.FileInfo().Mode().Perm(); mode != staged.Mode {
		t.Errorf("mode before start = %v, want %v", mode, staged.Mode)
	}
}

func TestStartReturnsOnlyOnceACommandRunsWithNoRetry(t *testing.T) {
	p, env := newProvider(t), envFor(t)
	ref := machinesOn(t, p, env, image, "node01")[0]
	if err := p.StartMachine(ctx(t), ref); err != nil {
		t.Fatal(err)
	}

	// One attempt, straight after Start returned, through Docker itself.
	if _, errOut, code := docker(t, "exec", container.ContainerName(env, "node01"), "true"); code != 0 {
		t.Errorf("first docker exec after Start exited %d: %s", code, errOut)
	}
}

// TestExecCaptureMatchesDockersOwn compares what the provider captured with
// what docker exec prints for the same command. With the container running,
// the CLI's exit code is the command's own, so it is a fair witness here.
func TestExecCaptureMatchesDockersOwn(t *testing.T) {
	p, env := newProvider(t), envFor(t)
	ref := machinesOn(t, p, env, image, "node01")[0]
	if err := p.StartMachine(ctx(t), ref); err != nil {
		t.Fatal(err)
	}
	c := container.ContainerName(env, "node01")

	for name, script := range map[string]string{
		"no trailing newline, stderr, exit 7": `printf 'no-newline'; printf 'to-stderr' >&2; exit 7`,
		"output past a pipe buffer":           `yes 0123456789 | head -c 300000`,
		"bytes that are not UTF-8":            `printf '\377\376\000\001'`,
		"interleaved streams":                 `for i in 1 2 3; do echo out$i; echo err$i >&2; done`,
		"exit 126 and 127 are the command's":  `exit 127`,
	} {
		t.Run(name, func(t *testing.T) {
			res, err := p.Exec(ctx(t), provider.ExecRequest{
				Machine: ref, Argv: []string{"sh", "-c", script}, Timeout: time.Minute,
			})
			if err != nil {
				t.Fatal(err)
			}
			wantOut, wantErr, wantCode := docker(t, "exec", c, "sh", "-c", script)

			if !bytes.Equal(res.Stdout, []byte(wantOut)) {
				t.Errorf("stdout differs: provider %d bytes, docker %d bytes", len(res.Stdout), len(wantOut))
			}
			if !bytes.Equal(res.Stderr, []byte(wantErr)) {
				t.Errorf("stderr: provider %q, docker %q", res.Stderr, wantErr)
			}
			if res.ExitCode != wantCode {
				t.Errorf("exit code: provider %d, docker %d", res.ExitCode, wantCode)
			}
		})
	}
}

// TestACommandPrintingDaemonErrorTextKeepsItsExitCode is the case that rules
// out the docker CLI as a transport: a command can print exactly what the
// daemon prints on failure, and exit 1 as the daemon's client does.
func TestACommandPrintingDaemonErrorTextKeepsItsExitCode(t *testing.T) {
	p, env := newProvider(t), envFor(t)
	ref := machinesOn(t, p, env, image, "node01")[0]
	if err := p.StartMachine(ctx(t), ref); err != nil {
		t.Fatal(err)
	}

	res, err := p.Exec(ctx(t), provider.ExecRequest{
		Machine: ref, Timeout: time.Minute,
		Argv: []string{"sh", "-c", `echo "Error response from daemon: No such container: x" >&2; exit 1`},
	})
	if err != nil {
		t.Fatalf("the command's own output was mistaken for a daemon failure: %v", err)
	}
	if res.ExitCode != 1 {
		t.Errorf("exit code = %d, want 1", res.ExitCode)
	}
}

func TestABackgroundProcessReallyRunsAndReallyStops(t *testing.T) {
	p, env := newProvider(t), envFor(t)
	ref := machinesOn(t, p, env, image, "node01")[0]
	if err := p.StartMachine(ctx(t), ref); err != nil {
		t.Fatal(err)
	}
	c := container.ContainerName(env, "node01")

	err := p.StartBackground(ctx(t), provider.BackgroundRequest{Machine: ref, Name: "sleeper", Argv: []string{"sleep", "4242"}})
	if err != nil {
		t.Fatal(err)
	}
	// docker top lists processes from the host, needing no tools inside.
	if top := mustDocker(t, "top", c, "-o", "pid,args"); !strings.Contains(top, "sleep 4242") {
		t.Fatalf("docker top does not show the background process:\n%s", top)
	}

	if err := p.StopBackground(ctx(t), ref, "sleeper"); err != nil {
		t.Fatal(err)
	}
	if top := mustDocker(t, "top", c, "-o", "pid,args"); strings.Contains(top, "sleep 4242") {
		t.Errorf("the process survived StopBackground:\n%s", top)
	}
}

func TestDeletingAMachineRemovesItsContainerAndVolumes(t *testing.T) {
	// registry:2 declares a VOLUME, so its container gets an anonymous volume:
	// the classic leak when a container is removed without its volumes.
	const withVolume = "registry:2"
	if _, _, code := docker(t, "image", "inspect", withVolume); code != 0 {
		t.Skipf("%s is not present locally", withVolume)
	}

	p, env := newProvider(t), envFor(t)
	ref := machinesOn(t, p, env, withVolume, "store")[0]
	if err := p.StartMachine(ctx(t), ref); err != nil {
		t.Fatal(err)
	}
	c := container.ContainerName(env, "store")

	volumes := strings.Fields(mustDocker(t, "inspect", "-f",
		`{{range .Mounts}}{{if eq .Type "volume"}}{{.Name}} {{end}}{{end}}`, c))
	if len(volumes) == 0 {
		t.Fatal("the fixture container has no anonymous volume to leak")
	}

	if err := p.DeleteMachine(ctx(t), ref); err != nil {
		t.Fatal(err)
	}
	if _, _, code := docker(t, "inspect", c); code == 0 {
		t.Errorf("container %s still exists", c)
	}
	for _, v := range volumes {
		if _, _, code := docker(t, "volume", "inspect", v); code == 0 {
			t.Errorf("volume %s outlived its Machine", v)
		}
	}
}

func TestDeleteRefusesAContainerTheEngineDidNotCreate(t *testing.T) {
	p, env := newProvider(t), envFor(t)
	impostor := container.ContainerName(env, "impostor")
	mustDocker(t, "create", "--name", impostor, image, "true") // no labels
	t.Cleanup(func() { docker(t, "rm", "-f", impostor) })

	err := p.DeleteMachine(ctx(t), provider.MachineRef{Environment: env, Machine: "impostor"})
	if err == nil {
		t.Error("deleted a container that carries no Environment label")
	}
	if _, _, code := docker(t, "inspect", impostor); code != 0 {
		t.Error("the unlabelled container was removed")
	}
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}
