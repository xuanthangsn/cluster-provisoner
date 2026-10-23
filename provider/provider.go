// Package provider defines the Machine provider: the single interface below
// which a backend lives.
//
// Everything above this interface is backend-neutral (NFR-9). A new backend —
// the VM backend of engine v2, say — is added by implementing Provider and
// passing the conformance suite in provider/conformance, without editing the
// executor. An architecture test fails the build if a package above this line
// references Docker.
//
// The interface is deliberately small, and deliberately says nothing about how
// a backend works: there are no container IDs, no image pulls and no backend
// error types here. Machines and networks are addressed by the names the
// manifest gives them, scoped by Environment. Nothing in it knows what
// Kubernetes is.
package provider

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"time"
)

// Provider creates, runs commands on, copies files to and from, and deletes
// Machines.
//
// Delete and Stop operations are idempotent: removing something that does not
// exist succeeds, so that cleanup never needs a guard. Every method honours
// its context; a backend also bounds every call of its own, so that no
// operation can hang (NFR-5).
type Provider interface {
	// Capabilities lists what this backend can supply. A manifest needing one
	// that is absent is rejected by the planner before anything runs (FR-8).
	Capabilities() []Capability

	// CreateNetwork creates a network. It fails with ErrAlreadyExists when the
	// Environment already has a network of that name.
	CreateNetwork(ctx context.Context, n Network) error

	// DeleteNetwork removes a network. Machines still attached to it must be
	// deleted first.
	DeleteNetwork(ctx context.Context, ref NetworkRef) error

	// CreateMachine creates a Machine without starting it, with every file in
	// m.Files already in place, so that whatever the Machine runs on its first
	// start reads them. It fails with ErrAlreadyExists for a duplicate name,
	// and with ErrNotFound when m.Network does not exist.
	CreateMachine(ctx context.Context, m Machine) error

	// StartMachine starts a created Machine and returns only once it is
	// reachable: when it returns nil, Exec on the Machine succeeds at once,
	// with no retry. Starting a running Machine succeeds.
	StartMachine(ctx context.Context, ref MachineRef) error

	// DeleteMachine removes a Machine, running or not, with its volumes and
	// any background processes.
	DeleteMachine(ctx context.Context, ref MachineRef) error

	// Exec runs a command on a running Machine and waits for it.
	//
	// A command that runs is never an error, whatever its exit code: the
	// result reports it. A command that outlives req.Timeout is reported with
	// TimedOut set. An error means the command could not be run at all — the
	// Machine is missing (ErrNotFound), not running (ErrNotRunning), or the
	// backend failed.
	Exec(ctx context.Context, req ExecRequest) (ExecResult, error)

	// StartBackground starts a long-running process on a running Machine,
	// does not wait for it, and returns once it has started. The name is the
	// handle StopBackground takes. Starting a name already running fails with
	// ErrAlreadyExists.
	StartBackground(ctx context.Context, req BackgroundRequest) error

	// StopBackground stops a background process. Stopping one that is not
	// running succeeds.
	StopBackground(ctx context.Context, ref MachineRef, name string) error

	// CopyIn writes a file into a created or running Machine, creating parent
	// directories as needed.
	CopyIn(ctx context.Context, ref MachineRef, f File) error

	// CopyOut reads a file from a created or running Machine. It fails with
	// ErrNotFound when the Machine or the path does not exist.
	CopyOut(ctx context.Context, ref MachineRef, path string) (File, error)

	// List reports everything this backend holds for an Environment. It is
	// the backend's own account, built from what exists rather than from any
	// record the engine kept, so that a lost state directory never strands
	// resources (NFR-7).
	List(ctx context.Context, environment string) (Inventory, error)
}

// Capability is something a backend can supply.
type Capability string

// Capabilities the PRD names. All three need a kernel of the Machine's own,
// so the container backend supplies none of them. Planner enforcement is
// issue 15.
const (
	CapabilityKernelModules Capability = "kernel-modules"
	CapabilitySwap          Capability = "swap"
	CapabilityClock         Capability = "clock"
)

// MachineRef names a Machine within an Environment.
type MachineRef struct {
	Environment string
	Machine     string
}

// NetworkRef names a network within an Environment.
type NetworkRef struct {
	Environment string
	Network     string
}

// Network is a network to create.
type Network struct {
	Environment string
	Name        string
	// Subnet is an IPv4 CIDR. Machines on the network take static addresses
	// from it.
	Subnet string
}

// Machine is a Machine to create.
type Machine struct {
	Environment string
	// Name is the Machine's name within its Environment, and also its
	// hostname (FR-11).
	Name string
	// Image is the image reference to run, as resolved by the catalog.
	Image string
	// Network is the name of the Environment network to join.
	Network string
	// IP is the Machine's static address on Network.
	IP string
	// Files are written into the Machine before it first starts.
	Files []File
}

// Ref returns the Machine's reference.
func (m Machine) Ref() MachineRef { return MachineRef{Environment: m.Environment, Machine: m.Name} }

// File is a file inside a Machine.
type File struct {
	// Path is absolute.
	Path string
	Data []byte
	// Mode holds the permission bits.
	Mode fs.FileMode
}

// ExecRequest is a command to run on a Machine.
type ExecRequest struct {
	Machine MachineRef
	// Argv is run directly, not through a shell; callers wanting a shell say
	// so, as in []string{"bash", "-c", script}.
	Argv []string
	// User runs the command as this user; empty means root.
	User string
	// Env adds environment variables.
	Env map[string]string
	// Timeout bounds the command and must be positive.
	Timeout time.Duration
	// Stdout and Stderr, when set, receive the command's output as it
	// arrives, in addition to its being captured in the result.
	Stdout io.Writer
	Stderr io.Writer
}

// ExecResult is what a command did.
type ExecResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
	// TimedOut reports that the command did not finish within its timeout.
	// The output captured until then is kept, and ExitCode is meaningless.
	TimedOut bool
}

// BackgroundRequest is a long-running process to start on a Machine.
type BackgroundRequest struct {
	Machine MachineRef
	// Name is the process's handle, unique among the Machine's background
	// processes.
	Name string
	Argv []string
	User string
	Env  map[string]string
}

// Inventory is what a backend holds for one Environment. Each list is sorted.
type Inventory struct {
	Machines []MachineInfo
	Networks []string
	Volumes  []string
}

// Empty reports whether the Environment holds nothing at all.
func (i Inventory) Empty() bool {
	return len(i.Machines) == 0 && len(i.Networks) == 0 && len(i.Volumes) == 0
}

// MachineInfo describes one existing Machine.
type MachineInfo struct {
	Name  string
	State State
}

// State is a Machine's lifecycle state.
type State string

// Machine states.
const (
	StateCreated State = "created"
	StateRunning State = "running"
	StateStopped State = "stopped"
)

// Errors a backend reports, wrapped with context. Callers test them with
// errors.Is. A backend never wraps its own error types alongside these, so no
// backend type can leak to a caller through errors.As.
var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrNotRunning    = errors.New("machine is not running")
	ErrUnavailable   = errors.New("backend unavailable")
)
