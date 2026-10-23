// Package container is the Machine provider whose Machines are Docker
// containers on the local host.
//
// It is the only package in the engine that knows Docker exists. It speaks the
// Engine API over the daemon's Unix socket (see client.go for why not the
// CLI), and it translates everything Docker says into the provider package's
// vocabulary before returning it.
//
// Every resource it creates is labelled with its Environment, and List works
// from those labels, so what exists is rebuilt from Docker itself rather than
// from any record the engine kept (NFR-7). It refuses to delete anything that
// carries a different Environment's label, or none: the host may run unrelated
// workloads with colliding names.
//
// Known limits of this release:
//
//   - A Machine runs `tail -f /dev/null` under Docker's init in place of its
//     image's own command, which keeps a plain Linux image alive. An image that
//     needs its own init — systemd in a Kubernetes node image — needs a
//     different start, which is issue 19.
//   - A command that outlives its timeout is reported as timed out, but keeps
//     running inside the Machine: closing the API stream does not stop it.
//     Killing it belongs with retries (issues 05 and 06).
//   - Background processes are tracked by pid file and stopped with signals.
//     Issue 07 may move them to transient systemd units where systemd runs.
package container

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"sim.dev/engine/provider"
)

// Labels on every resource this provider creates.
const (
	labelManaged     = "dev.sim.managed"
	labelEnvironment = "dev.sim.environment"
	labelMachine     = "dev.sim.machine"
	labelNetwork     = "dev.sim.network"
)

// Bounds on the provider's own waiting.
const (
	pullTimeout      = 15 * time.Minute
	reachableTimeout = time.Minute
	probeTimeout     = 10 * time.Second
	pollInterval     = 100 * time.Millisecond
)

// backgroundDir holds the pid files of background processes.
const backgroundDir = "/run/sim-bg"

var validBackgroundName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// Options configure the provider.
type Options struct {
	// Socket is the Docker daemon's Unix socket. It defaults to the path in a
	// unix:// DOCKER_HOST, then to /var/run/docker.sock.
	Socket string
}

// Provider runs Machines as Docker containers.
type Provider struct {
	api *client
}

var _ provider.Provider = (*Provider)(nil)

// New returns a provider talking to the local Docker daemon. It does not
// contact the daemon; the first call does.
func New(opts Options) (*Provider, error) {
	socket := opts.Socket
	if socket == "" {
		switch host := os.Getenv("DOCKER_HOST"); {
		case host == "":
			socket = "/var/run/docker.sock"
		case strings.HasPrefix(host, "unix://"):
			socket = strings.TrimPrefix(host, "unix://")
		default:
			return nil, fmt.Errorf("DOCKER_HOST=%s: only a local Unix socket is supported", host)
		}
	}
	return &Provider{api: newClient(socket)}, nil
}

// ContainerName is the Docker name of a Machine's container. The separator
// cannot occur in a DNS label, so no two Environments' names can collide.
func ContainerName(env, machine string) string { return env + "_" + machine }

// NetworkName is the Docker name of an Environment network.
func NetworkName(env, network string) string { return env + "_" + network }

// EnvironmentLabel is the label key naming a resource's Environment.
func EnvironmentLabel() string { return labelEnvironment }

// Capabilities reports none: every Capability the PRD names needs a kernel
// of the Machine's own, which a container does not have.
func (p *Provider) Capabilities() []provider.Capability { return nil }

// CreateNetwork creates a labelled bridge network.
func (p *Provider) CreateNetwork(ctx context.Context, n provider.Network) error {
	name := NetworkName(n.Environment, n.Name)
	body := map[string]any{
		"Name":   name,
		"Driver": "bridge",
		"Labels": map[string]string{
			labelManaged: "true", labelEnvironment: n.Environment, labelNetwork: n.Name,
		},
		"IPAM": map[string]any{
			"Driver": "default",
			"Config": []map[string]string{{"Subnet": n.Subnet}},
		},
	}
	err := p.api.json(ctx, http.MethodPost, "/networks/create", nil, body, nil)
	return translate(err, "creating network "+n.Name, map[int]error{http.StatusConflict: provider.ErrAlreadyExists})
}

// DeleteNetwork removes a network this provider created for the Environment.
func (p *Provider) DeleteNetwork(ctx context.Context, ref provider.NetworkRef) error {
	name := NetworkName(ref.Environment, ref.Network)
	op := "deleting network " + ref.Network

	var info struct{ Labels map[string]string }
	err := p.api.json(ctx, http.MethodGet, "/networks/"+name, nil, nil, &info)
	if statusOf(err) == http.StatusNotFound {
		return nil
	}
	if err != nil {
		return translate(err, op, nil)
	}
	if info.Labels[labelEnvironment] != ref.Environment {
		return fmt.Errorf("%s: Docker network %s was not created for Environment %s; refusing to delete it",
			op, name, ref.Environment)
	}

	err = p.api.json(ctx, http.MethodDelete, "/networks/"+name, nil, nil, nil)
	if statusOf(err) == http.StatusNotFound {
		return nil
	}
	return translate(err, op, nil)
}

// CreateMachine creates a container, writes its files, and leaves it created
// but not started. If writing a file fails the container is removed, so a
// half-built Machine is never left behind.
func (p *Provider) CreateMachine(ctx context.Context, m provider.Machine) error {
	op := "creating machine " + m.Name
	network := NetworkName(m.Environment, m.Network)

	// Check the network first, so a missing one is reported as such rather
	// than as whatever the container create happens to say.
	err := p.api.json(ctx, http.MethodGet, "/networks/"+network, nil, nil, nil)
	if statusOf(err) == http.StatusNotFound {
		return fmt.Errorf("%s: network %s: %w", op, m.Network, provider.ErrNotFound)
	}
	if err != nil {
		return translate(err, op, nil)
	}

	if err := p.ensureImage(ctx, m.Image); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	name := ContainerName(m.Environment, m.Name)
	body := map[string]any{
		"Image":    m.Image,
		"Hostname": m.Name,
		// Keep a plain image alive; see the package comment.
		"Entrypoint": []string{"tail", "-f", "/dev/null"},
		"Cmd":        nil,
		"Labels": map[string]string{
			labelManaged: "true", labelEnvironment: m.Environment, labelMachine: m.Name,
		},
		"HostConfig": map[string]any{
			// An init as PID 1 reaps orphans, so a stopped background
			// process does not linger as a zombie.
			"Init":        true,
			"NetworkMode": network,
		},
		"NetworkingConfig": map[string]any{
			"EndpointsConfig": map[string]any{
				network: map[string]any{
					"IPAMConfig": map[string]string{"IPv4Address": m.IP},
					"Aliases":    []string{m.Name},
				},
			},
		},
	}
	q := url.Values{"name": {name}}
	err = p.api.json(ctx, http.MethodPost, "/containers/create", q, body, nil)
	if err != nil {
		return translate(err, op, map[int]error{
			http.StatusConflict: provider.ErrAlreadyExists,
			http.StatusNotFound: provider.ErrNotFound,
		})
	}

	for _, f := range m.Files {
		if err := p.CopyIn(ctx, m.Ref(), f); err != nil {
			_ = p.remove(context.WithoutCancel(ctx), name)
			return fmt.Errorf("%s: staging %s: %w", op, f.Path, err)
		}
	}
	return nil
}

// ensureImage pulls an image unless it is already present.
func (p *Provider) ensureImage(ctx context.Context, ref string) error {
	err := p.api.json(ctx, http.MethodGet, "/images/"+ref+"/json", nil, nil, nil)
	if err == nil {
		return nil
	}
	if statusOf(err) != http.StatusNotFound {
		return translate(err, "inspecting image "+ref, nil)
	}

	ctx, cancel := bounded(ctx, pullTimeout)
	defer cancel()

	q := url.Values{"fromImage": {ref}}
	if !hasTagOrDigest(ref) {
		q.Set("tag", "latest") // without a tag, the API pulls every tag
	}
	resp, err := p.api.do(ctx, http.MethodPost, "/images/create", q, nil, "")
	if err != nil {
		return translate(err, "pulling image "+ref, map[int]error{http.StatusNotFound: provider.ErrNotFound})
	}
	defer resp.Body.Close()

	// A pull reports failure inside a 200 response, as a line of its stream.
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var line struct{ Error string }
		if json.Unmarshal(sc.Bytes(), &line) == nil && line.Error != "" {
			return fmt.Errorf("pulling image %s: %s", ref, line.Error)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("pulling image %s: %v", ref, err)
	}
	return nil
}

func hasTagOrDigest(ref string) bool {
	if strings.Contains(ref, "@") {
		return true
	}
	return strings.Contains(ref[strings.LastIndex(ref, "/")+1:], ":")
}

// StartMachine starts a container and waits until a command runs in it.
func (p *Provider) StartMachine(ctx context.Context, ref provider.MachineRef) error {
	op := "starting machine " + ref.Machine
	name := ContainerName(ref.Environment, ref.Machine)

	err := p.api.json(ctx, http.MethodPost, "/containers/"+name+"/start", nil, nil, nil)
	if statusOf(err) == http.StatusNotModified {
		err = nil // already running
	}
	if err != nil {
		return translate(err, op, map[int]error{http.StatusNotFound: provider.ErrNotFound})
	}

	// The interface promises that once this returns, exec works at once.
	// Prove it here so callers never have to retry.
	ctx, cancel := context.WithTimeout(ctx, reachableTimeout)
	defer cancel()
	var last error
	for {
		res, err := p.Exec(ctx, provider.ExecRequest{Machine: ref, Argv: []string{"true"}, Timeout: probeTimeout})
		if err == nil && res.ExitCode == 0 && !res.TimedOut {
			return nil
		}
		if err != nil {
			last = err
		} else {
			last = fmt.Errorf("probe exited %d", res.ExitCode)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: not reachable within %s: %v", op, reachableTimeout, last)
		case <-time.After(pollInterval):
		}
	}
}

// DeleteMachine force-removes a container with its anonymous volumes.
func (p *Provider) DeleteMachine(ctx context.Context, ref provider.MachineRef) error {
	op := "deleting machine " + ref.Machine
	name := ContainerName(ref.Environment, ref.Machine)

	var info struct {
		Config struct{ Labels map[string]string }
	}
	err := p.api.json(ctx, http.MethodGet, "/containers/"+name+"/json", nil, nil, &info)
	if statusOf(err) == http.StatusNotFound {
		return nil
	}
	if err != nil {
		return translate(err, op, nil)
	}
	if info.Config.Labels[labelEnvironment] != ref.Environment {
		return fmt.Errorf("%s: Docker container %s was not created for Environment %s; refusing to delete it",
			op, name, ref.Environment)
	}
	return translate(p.remove(ctx, name), op, nil)
}

// remove force-removes a container and its anonymous volumes. Removing one
// that is gone succeeds.
func (p *Provider) remove(ctx context.Context, name string) error {
	q := url.Values{"force": {"true"}, "v": {"true"}}
	err := p.api.json(ctx, http.MethodDelete, "/containers/"+name, q, nil, nil)
	if statusOf(err) == http.StatusNotFound {
		return nil
	}
	return err
}

// Exec runs a command and waits for it.
func (p *Provider) Exec(ctx context.Context, req provider.ExecRequest) (provider.ExecResult, error) {
	op := fmt.Sprintf("exec on %s", req.Machine.Machine)
	if req.Timeout <= 0 {
		return provider.ExecResult{}, fmt.Errorf("%s: a positive timeout is required", op)
	}
	if len(req.Argv) == 0 {
		return provider.ExecResult{}, fmt.Errorf("%s: no command given", op)
	}

	id, err := p.createExec(ctx, req.Machine, req.Argv, req.User, req.Env, true)
	if err != nil {
		return provider.ExecResult{}, translate(err, op, execErrors)
	}

	execCtx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	streamErr := p.streamExec(execCtx, id, tee(&stdout, req.Stdout), tee(&stderr, req.Stderr))

	result := provider.ExecResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if streamErr != nil {
		if ctx.Err() != nil {
			return provider.ExecResult{}, ctx.Err()
		}
		if execCtx.Err() != nil {
			result.TimedOut = true
			return result, nil
		}
		return provider.ExecResult{}, translate(streamErr, op, nil)
	}

	code, err := p.exitCode(ctx, id)
	if err != nil {
		return provider.ExecResult{}, translate(err, op, nil)
	}
	result.ExitCode = code
	return result, nil
}

// execErrors maps the daemon's refusals of an exec create.
var execErrors = map[int]error{
	http.StatusNotFound: provider.ErrNotFound,
	http.StatusConflict: provider.ErrNotRunning,
}

func (p *Provider) createExec(ctx context.Context, ref provider.MachineRef, argv []string, user string, env map[string]string, attach bool) (string, error) {
	if user == "" {
		user = "0" // root, even if the image sets a different USER
	}
	vars := make([]string, 0, len(env))
	for k, v := range env {
		vars = append(vars, k+"="+v)
	}
	sort.Strings(vars)

	body := map[string]any{
		"AttachStdout": attach,
		"AttachStderr": attach,
		"Cmd":          argv,
		"User":         user,
		"Env":          vars,
	}
	var created struct {
		ID string `json:"Id"`
	}
	err := p.api.json(ctx, http.MethodPost, "/containers/"+ContainerName(ref.Environment, ref.Machine)+"/exec", nil, body, &created)
	return created.ID, err
}

// streamExec starts an attached exec and copies its multiplexed output.
func (p *Provider) streamExec(ctx context.Context, id string, stdout, stderr io.Writer) error {
	body := strings.NewReader(`{"Detach":false,"Tty":false}`)
	resp, err := p.api.do(ctx, http.MethodPost, "/exec/"+id+"/start", nil, body, "application/json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return demux(resp.Body, stdout, stderr)
}

// demux splits the Engine API's multiplexed stream: each frame is an 8-byte
// header — stream type, three zero bytes, a big-endian payload length — and
// then the payload.
func demux(r io.Reader, stdout, stderr io.Writer) error {
	var hdr [8]byte
	for {
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		size := int64(binary.BigEndian.Uint32(hdr[4:]))
		var dst io.Writer
		switch hdr[0] {
		case 1:
			dst = stdout
		case 2:
			dst = stderr
		default:
			dst = io.Discard
		}
		if _, err := io.CopyN(dst, r, size); err != nil {
			return err
		}
	}
}

// exitCode reads a finished exec's exit code. The stream can close a moment
// before the daemon records the exit, so a still-running exec is polled.
func (p *Provider) exitCode(ctx context.Context, id string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	for {
		var info struct {
			Running  bool
			ExitCode int
		}
		if err := p.api.json(ctx, http.MethodGet, "/exec/"+id+"/json", nil, nil, &info); err != nil {
			return 0, err
		}
		if !info.Running {
			return info.ExitCode, nil
		}
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("exec %s still running after its output closed", id)
		case <-time.After(pollInterval):
		}
	}
}

// StartBackground starts a detached process that records its pid, so that
// StopBackground can find it.
func (p *Provider) StartBackground(ctx context.Context, req provider.BackgroundRequest) error {
	op := fmt.Sprintf("starting background process %s on %s", req.Name, req.Machine.Machine)
	if !validBackgroundName.MatchString(req.Name) {
		return fmt.Errorf("%s: invalid name", op)
	}
	if len(req.Argv) == 0 {
		return fmt.Errorf("%s: no command given", op)
	}
	pidFile := backgroundDir + "/" + req.Name + ".pid"

	if alive, err := p.backgroundAlive(ctx, req.Machine, pidFile); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	} else if alive {
		return fmt.Errorf("%s: %w", op, provider.ErrAlreadyExists)
	}

	// The directory is world-writable so a process started as any user can
	// record itself; StopBackground runs as root.
	if err := p.runRoot(ctx, req.Machine, "mkdir -p "+backgroundDir+" && chmod 1777 "+backgroundDir); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	argv := append([]string{"sh", "-c", `echo $$ > "$0" && exec "$@"`, pidFile}, req.Argv...)
	id, err := p.createExec(ctx, req.Machine, argv, req.User, req.Env, false)
	if err != nil {
		return translate(err, op, execErrors)
	}
	err = p.api.json(ctx, http.MethodPost, "/exec/"+id+"/start", nil, map[string]bool{"Detach": true, "Tty": false}, nil)
	if err != nil {
		return translate(err, op, nil)
	}

	// Return only once the process has started, so a Stop straight after
	// finds it.
	waitCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	for {
		alive, err := p.backgroundAlive(waitCtx, req.Machine, pidFile)
		if err == nil && alive {
			return nil
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("%s: did not start within %s", op, probeTimeout)
		case <-time.After(pollInterval):
		}
	}
}

// StopBackground signals a background process to stop, escalating to SIGKILL
// after five seconds.
func (p *Provider) StopBackground(ctx context.Context, ref provider.MachineRef, name string) error {
	op := fmt.Sprintf("stopping background process %s on %s", name, ref.Machine)
	if !validBackgroundName.MatchString(name) {
		return fmt.Errorf("%s: invalid name", op)
	}
	pidFile := backgroundDir + "/" + name + ".pid"
	script := `f=` + pidFile + `
[ -f "$f" ] || exit 0
pid=$(cat "$f")
kill -TERM "$pid" 2>/dev/null
i=0
while kill -0 "$pid" 2>/dev/null && [ "$i" -lt 50 ]; do sleep 0.1; i=$((i+1)); done
kill -KILL "$pid" 2>/dev/null
rm -f "$f"`

	err := p.runRoot(ctx, ref, script)
	if errors.Is(err, provider.ErrNotRunning) {
		return nil // a stopped Machine runs nothing
	}
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

// backgroundAlive reports whether a pid file names a live process.
func (p *Provider) backgroundAlive(ctx context.Context, ref provider.MachineRef, pidFile string) (bool, error) {
	res, err := p.Exec(ctx, provider.ExecRequest{
		Machine: ref,
		Argv:    []string{"sh", "-c", `[ -s "$0" ] && kill -0 "$(cat "$0")" 2>/dev/null`, pidFile},
		Timeout: probeTimeout,
	})
	if err != nil {
		return false, err
	}
	return res.ExitCode == 0 && !res.TimedOut, nil
}

// runRoot runs a shell script as root and requires it to succeed.
func (p *Provider) runRoot(ctx context.Context, ref provider.MachineRef, script string) error {
	res, err := p.Exec(ctx, provider.ExecRequest{Machine: ref, Argv: []string{"sh", "-c", script}, Timeout: probeTimeout})
	if err != nil {
		return err
	}
	if res.TimedOut || res.ExitCode != 0 {
		return fmt.Errorf("internal command failed (exit %d): %s", res.ExitCode, bytes.TrimSpace(res.Stderr))
	}
	return nil
}

// CopyIn writes a file through the archive endpoint, which works whether or
// not the container has started. Missing parent directories are created.
func (p *Provider) CopyIn(ctx context.Context, ref provider.MachineRef, f provider.File) error {
	op := fmt.Sprintf("copying %s into %s", f.Path, ref.Machine)
	if !strings.HasPrefix(f.Path, "/") {
		return fmt.Errorf("%s: the path must be absolute", op)
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     strings.TrimPrefix(f.Path, "/"),
		Mode:     int64(f.Mode.Perm()),
		Size:     int64(len(f.Data)),
		ModTime:  time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("%s: %v", op, err)
	}
	if _, err := tw.Write(f.Data); err != nil {
		return fmt.Errorf("%s: %v", op, err)
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("%s: %v", op, err)
	}

	ctx, cancel := bounded(ctx, defaultCallTimeout)
	defer cancel()
	q := url.Values{"path": {"/"}}
	resp, err := p.api.do(ctx, http.MethodPut, "/containers/"+ContainerName(ref.Environment, ref.Machine)+"/archive",
		q, &buf, "application/x-tar")
	if err != nil {
		return translate(err, op, map[int]error{http.StatusNotFound: provider.ErrNotFound})
	}
	resp.Body.Close()
	return nil
}

// CopyOut reads one regular file through the archive endpoint.
func (p *Provider) CopyOut(ctx context.Context, ref provider.MachineRef, path string) (provider.File, error) {
	op := fmt.Sprintf("copying %s out of %s", path, ref.Machine)

	ctx, cancel := bounded(ctx, defaultCallTimeout)
	defer cancel()
	q := url.Values{"path": {path}}
	resp, err := p.api.do(ctx, http.MethodGet, "/containers/"+ContainerName(ref.Environment, ref.Machine)+"/archive",
		q, nil, "")
	if err != nil {
		return provider.File{}, translate(err, op, map[int]error{http.StatusNotFound: provider.ErrNotFound})
	}
	defer resp.Body.Close()

	tr := tar.NewReader(resp.Body)
	hdr, err := tr.Next()
	if err != nil {
		return provider.File{}, fmt.Errorf("%s: reading archive: %v", op, err)
	}
	if hdr.Typeflag != tar.TypeReg {
		return provider.File{}, fmt.Errorf("%s: not a regular file", op)
	}
	data, err := io.ReadAll(tr)
	if err != nil {
		return provider.File{}, fmt.Errorf("%s: reading archive: %v", op, err)
	}
	return provider.File{Path: path, Data: data, Mode: fs.FileMode(hdr.Mode).Perm()}, nil
}

// List reports the Environment's containers, networks and volumes, found by
// label.
func (p *Provider) List(ctx context.Context, environment string) (provider.Inventory, error) {
	op := "listing " + environment
	filter := labelFilter(labelEnvironment, environment)

	var containers []struct {
		State  string
		Labels map[string]string
		Mounts []struct{ Type, Name string }
	}
	q := url.Values{"all": {"true"}, "filters": filter["filters"]}
	if err := p.api.json(ctx, http.MethodGet, "/containers/json", q, nil, &containers); err != nil {
		return provider.Inventory{}, translate(err, op, nil)
	}

	var networks []struct{ Labels map[string]string }
	if err := p.api.json(ctx, http.MethodGet, "/networks", filter, nil, &networks); err != nil {
		return provider.Inventory{}, translate(err, op, nil)
	}

	var volumes struct{ Volumes []struct{ Name string } }
	if err := p.api.json(ctx, http.MethodGet, "/volumes", filter, nil, &volumes); err != nil {
		return provider.Inventory{}, translate(err, op, nil)
	}

	var inv provider.Inventory
	volumeSet := map[string]bool{}
	for _, c := range containers {
		inv.Machines = append(inv.Machines, provider.MachineInfo{Name: c.Labels[labelMachine], State: state(c.State)})
		for _, m := range c.Mounts {
			if m.Type == "volume" && m.Name != "" {
				volumeSet[m.Name] = true
			}
		}
	}
	for _, n := range networks {
		inv.Networks = append(inv.Networks, n.Labels[labelNetwork])
	}
	for _, v := range volumes.Volumes {
		volumeSet[v.Name] = true
	}
	for v := range volumeSet {
		inv.Volumes = append(inv.Volumes, v)
	}

	sort.Slice(inv.Machines, func(i, j int) bool { return inv.Machines[i].Name < inv.Machines[j].Name })
	sort.Strings(inv.Networks)
	sort.Strings(inv.Volumes)
	return inv, nil
}

func state(docker string) provider.State {
	switch docker {
	case "created":
		return provider.StateCreated
	case "running":
		return provider.StateRunning
	}
	return provider.StateStopped
}

// translate turns a client error into the provider vocabulary. A daemon
// refusal whose status appears in kinds wraps that provider error; anything
// else keeps only Docker's message, so no Docker-side type escapes.
func translate(err error, op string, kinds map[int]error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, provider.ErrUnavailable) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w", op, err)
	}
	var ae *apiError
	if errors.As(err, &ae) {
		if kind, ok := kinds[ae.status]; ok {
			return fmt.Errorf("%s: %w: %s", op, kind, ae.message)
		}
		return fmt.Errorf("%s: %s", op, ae.Error())
	}
	return fmt.Errorf("%s: %s", op, err.Error())
}

// tee writes to w and, when it is non-nil, to extra.
func tee(w io.Writer, extra io.Writer) io.Writer {
	if extra == nil {
		return w
	}
	return io.MultiWriter(w, extra)
}
