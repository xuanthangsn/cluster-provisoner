// Package fake is an in-memory Machine provider.
//
// It exists so that everything above the provider — executor, task runner,
// check engine — can be tested with Docker stopped (NFR-10). It passes the
// same conformance suite as the container provider, which is what makes tests
// against it meaningful: it does not let the engine get away with anything a
// real backend would refuse.
//
// It keeps no record of the calls made to it, on purpose. Engine tests assert
// on reports and events, never on which provider methods were called.
//
// Commands are not executed. A small vocabulary is interpreted against the
// fake's in-memory files — see defaultExec — and anything else fails with exit
// code 127, loudly, so that a test relying on an unmodelled command finds out.
// Tests needing other behaviour install a Handler.
package fake

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"

	"sim.dev/engine/provider"
)

// Op names a provider operation, for InjectError.
type Op string

// Operations an error can be injected into.
const (
	OpCreateNetwork Op = "create-network"
	OpCreateMachine Op = "create-machine"
	OpStartMachine  Op = "start-machine"
	OpDeleteMachine Op = "delete-machine"
	OpDeleteNetwork Op = "delete-network"
	OpExec          Op = "exec"
	OpList          Op = "list"
)

// Handler decides the result of a command. It returns false to fall through
// to the default vocabulary.
type Handler func(m Machine, req provider.ExecRequest) (provider.ExecResult, bool)

// Machine is a read-only view of a fake Machine, given to a Handler.
type Machine struct {
	Name  string
	Files map[string]provider.File
}

// Provider is the in-memory provider. Its zero value is not usable; call New.
type Provider struct {
	mu       sync.Mutex
	caps     []provider.Capability
	networks map[provider.NetworkRef]provider.Network
	machines map[provider.MachineRef]*machine
	handler  Handler
	inject   map[Op]error
}

type machine struct {
	spec       provider.Machine
	state      provider.State
	files      map[string]provider.File
	background map[string]bool
}

var _ provider.Provider = (*Provider)(nil)

// New returns an empty fake supplying caps.
func New(caps ...provider.Capability) *Provider {
	return &Provider{
		caps:     caps,
		networks: map[provider.NetworkRef]provider.Network{},
		machines: map[provider.MachineRef]*machine{},
		inject:   map[Op]error{},
	}
}

// SetHandler installs a Handler consulted before the default vocabulary.
func (p *Provider) SetHandler(h Handler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handler = h
}

// InjectError makes every later call of op fail with err, so a test can drive
// the engine down its failure paths. A nil err removes the injection.
func (p *Provider) InjectError(op Op, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err == nil {
		delete(p.inject, op)
		return
	}
	p.inject[op] = err
}

func (p *Provider) injected(op Op) error { return p.inject[op] }

// Capabilities returns the capabilities New was given.
func (p *Provider) Capabilities() []provider.Capability {
	return append([]provider.Capability(nil), p.caps...)
}

// CreateNetwork records a network, refusing a duplicate name or a subnet that
// overlaps any existing network, as a real backend would.
func (p *Provider) CreateNetwork(ctx context.Context, n provider.Network) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.injected(OpCreateNetwork); err != nil {
		return err
	}

	ref := provider.NetworkRef{Environment: n.Environment, Network: n.Name}
	if _, dup := p.networks[ref]; dup {
		return fmt.Errorf("network %s: %w", n.Name, provider.ErrAlreadyExists)
	}
	subnet, err := netip.ParsePrefix(n.Subnet)
	if err != nil {
		return fmt.Errorf("network %s: subnet %q: %v", n.Name, n.Subnet, err)
	}
	for _, other := range p.networks {
		if o, err := netip.ParsePrefix(other.Subnet); err == nil && o.Overlaps(subnet) {
			return fmt.Errorf("network %s: subnet %s overlaps %s of network %s/%s",
				n.Name, n.Subnet, other.Subnet, other.Environment, other.Name)
		}
	}
	p.networks[ref] = n
	return nil
}

// DeleteNetwork removes a network. It refuses while Machines are attached.
func (p *Provider) DeleteNetwork(ctx context.Context, ref provider.NetworkRef) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.injected(OpDeleteNetwork); err != nil {
		return err
	}

	if _, ok := p.networks[ref]; !ok {
		return nil
	}
	for mref, m := range p.machines {
		if mref.Environment == ref.Environment && m.spec.Network == ref.Network {
			return fmt.Errorf("network %s: Machine %s is still attached", ref.Network, mref.Machine)
		}
	}
	delete(p.networks, ref)
	return nil
}

// CreateMachine records a created Machine with its files in place.
func (p *Provider) CreateMachine(ctx context.Context, m provider.Machine) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.injected(OpCreateMachine); err != nil {
		return err
	}

	ref := m.Ref()
	if _, dup := p.machines[ref]; dup {
		return fmt.Errorf("machine %s: %w", m.Name, provider.ErrAlreadyExists)
	}
	if _, ok := p.networks[provider.NetworkRef{Environment: m.Environment, Network: m.Network}]; !ok {
		return fmt.Errorf("machine %s: network %s: %w", m.Name, m.Network, provider.ErrNotFound)
	}

	files := make(map[string]provider.File, len(m.Files))
	for _, f := range m.Files {
		files[f.Path] = copyFile(f)
	}
	p.machines[ref] = &machine{
		spec:       m,
		state:      provider.StateCreated,
		files:      files,
		background: map[string]bool{},
	}
	return nil
}

// StartMachine marks a Machine running. A fake Machine is reachable at once.
func (p *Provider) StartMachine(ctx context.Context, ref provider.MachineRef) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.injected(OpStartMachine); err != nil {
		return err
	}

	m, ok := p.machines[ref]
	if !ok {
		return fmt.Errorf("machine %s: %w", ref.Machine, provider.ErrNotFound)
	}
	m.state = provider.StateRunning
	return nil
}

// DeleteMachine forgets a Machine, its files and its background processes.
func (p *Provider) DeleteMachine(ctx context.Context, ref provider.MachineRef) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.injected(OpDeleteMachine); err != nil {
		return err
	}
	delete(p.machines, ref)
	return nil
}

// Exec interprets a command; see the package comment.
func (p *Provider) Exec(ctx context.Context, req provider.ExecRequest) (provider.ExecResult, error) {
	if err := ctx.Err(); err != nil {
		return provider.ExecResult{}, err
	}
	if req.Timeout <= 0 {
		return provider.ExecResult{}, fmt.Errorf("exec on %s: a positive timeout is required", req.Machine.Machine)
	}

	p.mu.Lock()
	if err := p.injected(OpExec); err != nil {
		p.mu.Unlock()
		return provider.ExecResult{}, err
	}
	m, err := p.running(req.Machine)
	if err != nil {
		p.mu.Unlock()
		return provider.ExecResult{}, err
	}
	view := Machine{Name: m.spec.Name, Files: make(map[string]provider.File, len(m.files))}
	for k, f := range m.files {
		view.Files[k] = copyFile(f)
	}
	handler := p.handler
	p.mu.Unlock()

	var (
		result  provider.ExecResult
		handled bool
	)
	if handler != nil {
		result, handled = handler(view, req)
	}
	if !handled {
		result = defaultExec(view, req)
	}

	if req.Stdout != nil && len(result.Stdout) > 0 {
		_, _ = req.Stdout.Write(result.Stdout)
	}
	if req.Stderr != nil && len(result.Stderr) > 0 {
		_, _ = req.Stderr.Write(result.Stderr)
	}
	return result, nil
}

// StartBackground records a named background process.
func (p *Provider) StartBackground(ctx context.Context, req provider.BackgroundRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	m, err := p.running(req.Machine)
	if err != nil {
		return err
	}
	if m.background[req.Name] {
		return fmt.Errorf("background process %s on %s: %w", req.Name, req.Machine.Machine, provider.ErrAlreadyExists)
	}
	m.background[req.Name] = true
	return nil
}

// StopBackground forgets a named background process.
func (p *Provider) StopBackground(ctx context.Context, ref provider.MachineRef, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	m, ok := p.machines[ref]
	if !ok {
		return fmt.Errorf("machine %s: %w", ref.Machine, provider.ErrNotFound)
	}
	delete(m.background, name)
	return nil
}

// CopyIn stores a file in a created or running Machine.
func (p *Provider) CopyIn(ctx context.Context, ref provider.MachineRef, f provider.File) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	m, ok := p.machines[ref]
	if !ok {
		return fmt.Errorf("machine %s: %w", ref.Machine, provider.ErrNotFound)
	}
	m.files[f.Path] = copyFile(f)
	return nil
}

// CopyOut returns a stored file.
func (p *Provider) CopyOut(ctx context.Context, ref provider.MachineRef, path string) (provider.File, error) {
	if err := ctx.Err(); err != nil {
		return provider.File{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	m, ok := p.machines[ref]
	if !ok {
		return provider.File{}, fmt.Errorf("machine %s: %w", ref.Machine, provider.ErrNotFound)
	}
	f, ok := m.files[path]
	if !ok {
		return provider.File{}, fmt.Errorf("%s on %s: %w", path, ref.Machine, provider.ErrNotFound)
	}
	return copyFile(f), nil
}

// List reports the Environment's Machines and networks. The fake holds no
// volumes.
func (p *Provider) List(ctx context.Context, environment string) (provider.Inventory, error) {
	if err := ctx.Err(); err != nil {
		return provider.Inventory{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.injected(OpList); err != nil {
		return provider.Inventory{}, err
	}

	var inv provider.Inventory
	for ref, m := range p.machines {
		if ref.Environment == environment {
			inv.Machines = append(inv.Machines, provider.MachineInfo{Name: ref.Machine, State: m.state})
		}
	}
	for ref := range p.networks {
		if ref.Environment == environment {
			inv.Networks = append(inv.Networks, ref.Network)
		}
	}
	sort.Slice(inv.Machines, func(i, j int) bool { return inv.Machines[i].Name < inv.Machines[j].Name })
	sort.Strings(inv.Networks)
	return inv, nil
}

// running returns a Machine that exists and is running. p.mu must be held.
func (p *Provider) running(ref provider.MachineRef) (*machine, error) {
	m, ok := p.machines[ref]
	if !ok {
		return nil, fmt.Errorf("machine %s: %w", ref.Machine, provider.ErrNotFound)
	}
	if m.state != provider.StateRunning {
		return nil, fmt.Errorf("machine %s: %w", ref.Machine, provider.ErrNotRunning)
	}
	return m, nil
}

// defaultExec interprets the fake's command vocabulary:
//
//	true, false, hostname, id -un
//	cat PATH          — a file in the Machine
//	printf %s TEXT    — TEXT, verbatim
//	printenv NAME     — a variable from the request
//	sh|bash -c SCRIPT — where SCRIPT is true, false, exit N, or echo [TEXT]
//
// This is exactly what the conformance suite uses, plus the simplest scripts a
// manifest would carry. Anything else exits 127.
func defaultExec(m Machine, req provider.ExecRequest) provider.ExecResult {
	argv := req.Argv
	ok := func(out string) provider.ExecResult { return provider.ExecResult{Stdout: []byte(out)} }
	fail := func(code int, msg string) provider.ExecResult {
		return provider.ExecResult{ExitCode: code, Stderr: []byte(msg)}
	}

	switch {
	case len(argv) == 1 && argv[0] == "true":
		return ok("")
	case len(argv) == 1 && argv[0] == "false":
		return fail(1, "")
	case len(argv) == 1 && argv[0] == "hostname":
		return ok(m.Name + "\n")
	case len(argv) == 2 && argv[0] == "id" && argv[1] == "-un":
		if req.User == "" {
			return ok("root\n")
		}
		return ok(req.User + "\n")
	case len(argv) == 2 && argv[0] == "cat":
		f, found := m.Files[argv[1]]
		if !found {
			return fail(1, "cat: "+argv[1]+": No such file or directory\n")
		}
		return provider.ExecResult{Stdout: f.Data}
	case len(argv) == 3 && argv[0] == "printf" && argv[1] == "%s":
		return ok(argv[2])
	case len(argv) == 2 && argv[0] == "printenv":
		v, found := req.Env[argv[1]]
		if !found {
			return fail(1, "")
		}
		return ok(v + "\n")
	case len(argv) == 3 && (argv[0] == "sh" || argv[0] == "bash") && argv[1] == "-c":
		return script(argv[2])
	}
	return fail(127, "fake provider: unsupported command "+strconv.Quote(strings.Join(argv, " "))+"\n")
}

func script(s string) provider.ExecResult {
	s = strings.TrimSpace(s)
	switch {
	case s == "true":
		return provider.ExecResult{}
	case s == "false":
		return provider.ExecResult{ExitCode: 1}
	case s == "echo":
		return provider.ExecResult{Stdout: []byte("\n")}
	case strings.HasPrefix(s, "echo "):
		return provider.ExecResult{Stdout: []byte(strings.TrimPrefix(s, "echo ") + "\n")}
	case strings.HasPrefix(s, "exit "):
		if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(s, "exit "))); err == nil {
			return provider.ExecResult{ExitCode: n}
		}
	}
	return provider.ExecResult{
		ExitCode: 127,
		Stderr:   []byte("fake provider: unsupported script " + strconv.Quote(s) + "\n"),
	}
}

func copyFile(f provider.File) provider.File {
	f.Data = append([]byte(nil), f.Data...)
	return f
}
