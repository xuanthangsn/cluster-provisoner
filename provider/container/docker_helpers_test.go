//go:build docker

package container_test

import (
	"bytes"
	"net/netip"
	"os/exec"
	"strings"
	"testing"

	"sim.dev/engine/provider/container"
)

// image is the plain Linux image the Docker-level tests run.
const image = "ubuntu:24.04"

// docker runs the docker CLI. It is the out-of-band observer: what it reports
// is Docker's own account, independent of the provider under test.
func docker(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errb bytes.Buffer
	cmd := exec.Command("docker", args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		return out.String(), errb.String(), ee.ExitCode()
	}
	if err != nil {
		t.Fatalf("running docker %v: %v", args, err)
	}
	return out.String(), errb.String(), 0
}

// mustDocker runs the docker CLI and fails the test unless it succeeds.
func mustDocker(t *testing.T, args ...string) string {
	t.Helper()
	out, errOut, code := docker(t, args...)
	if code != 0 {
		t.Fatalf("docker %v exited %d: %s", args, code, errOut)
	}
	return out
}

func newProvider(t *testing.T) *container.Provider {
	t.Helper()
	p, err := container.New(container.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// freeSubnets returns n /24 subnets that overlap no network on this host. The
// host may run unrelated workloads, so this asks Docker rather than assuming.
func freeSubnets(t *testing.T, n int) []string {
	t.Helper()
	var taken []netip.Prefix
	ids := strings.Fields(mustDocker(t, "network", "ls", "-q"))
	if len(ids) > 0 {
		out := mustDocker(t, append([]string{"network", "inspect", "-f", "{{range .IPAM.Config}}{{.Subnet}} {{end}}"}, ids...)...)
		for _, s := range strings.Fields(out) {
			if p, err := netip.ParsePrefix(s); err == nil {
				taken = append(taken, p)
			}
		}
	}

	var free []string
	for third := 200; third < 255 && len(free) < n; third++ {
		candidate := netip.MustParsePrefix("172.31." + itoa(third) + ".0/24")
		clash := false
		for _, p := range taken {
			if p.Overlaps(candidate) {
				clash = true
				break
			}
		}
		if !clash {
			free = append(free, candidate.String())
		}
	}
	if len(free) < n {
		t.Fatalf("found only %d free subnets in 172.31.200.0-254.0", len(free))
	}
	return free
}

// purge removes everything labelled for env using the docker CLI, so that
// cleanup does not depend on the provider being correct.
func purge(t *testing.T, env string) {
	t.Helper()
	label := "label=" + container.EnvironmentLabel() + "=" + env
	if ids := strings.Fields(mustDocker(t, "ps", "-aq", "--filter", label)); len(ids) > 0 {
		docker(t, append([]string{"rm", "-fv"}, ids...)...)
	}
	if ids := strings.Fields(mustDocker(t, "network", "ls", "-q", "--filter", label)); len(ids) > 0 {
		docker(t, append([]string{"network", "rm"}, ids...)...)
	}
}

func itoa(n int) string {
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if s == "" {
		return "0"
	}
	return s
}
