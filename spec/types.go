package spec

import (
	"encoding/json"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// Recognised manifest header values.
const (
	// APIVersion is the only manifest version this engine understands.
	//
	// The group is a placeholder. prd.md records the engine's product name and
	// its final API group as an open question, to be settled before the JSON
	// Schema is published (issue 20); both change together when it is.
	APIVersion = "sim.dev/v1alpha2"

	// Kind is the only manifest kind.
	Kind = "Environment"
)

// Backends. Only one is provisioned in this release.
const (
	BackendContainer = "container"

	// BackendVM is reserved and rejected. It is named here so that a manifest
	// asking for it is told the backend is reserved rather than unrecognised.
	// Capability gating owns the richer message (issue 15).
	BackendVM = "vm"
)

// Task types. Only exec is implemented so far; transfer is issue 08.
const (
	TaskTypeExec = "exec"
)

// Documented defaults, applied before validation so that an author can see the
// manifest the engine actually works from (FR-2).
const (
	DefaultBackend  = BackendContainer
	DefaultTaskType = TaskTypeExec
	DefaultTimeout  = Duration(60 * time.Second)
	DefaultAttempts = 1
)

// Environment is a decoded, defaulted manifest.
type Environment struct {
	APIVersion string         `yaml:"apiVersion" json:"apiVersion"`
	Kind       string         `yaml:"kind"       json:"kind"`
	Name       string         `yaml:"name"       json:"name"`
	Backend    string         `yaml:"backend"    json:"backend"`
	Machines   []Machine      `yaml:"machines"   json:"machines"`
	Tasks      Section[Task]  `yaml:"tasks"      json:"tasks"`
	Checks     Section[Check] `yaml:"checks"     json:"checks"`
}

// Machine is one Linux system declared in the manifest. It becomes a
// Kubernetes node only if a Task bootstraps it.
//
// Multiple Machines from one declaration (names), networks, static IPs,
// resource limits and build contexts are issues 10 and 12.
type Machine struct {
	Name  string `yaml:"name"  json:"name"`
	Image string `yaml:"image" json:"image"`
}

// Task is one step. Only exec Tasks with a command are implemented here;
// Probes and wait Tasks are issue 05, retries issue 06, background issue 07,
// transfers issue 08 and templates issue 09.
type Task struct {
	Type     string   `yaml:"type"     json:"type"`
	Machine  string   `yaml:"machine"  json:"machine"`
	Run      string   `yaml:"run"      json:"run"`
	ExitCode int      `yaml:"exitCode" json:"exitCode"`
	Stdout   *Match   `yaml:"stdout"   json:"stdout,omitempty"`
	Timeout  Duration `yaml:"timeout"  json:"timeout"`
	Attempts int      `yaml:"attempts" json:"attempts"`
	Needs    []string `yaml:"needs"    json:"needs"`
}

// Check is an independent verification command. Checks take the same fields as
// an exec Task except type, background and needs. Running them is issue 03.
type Check struct {
	Machine  string   `yaml:"machine"  json:"machine"`
	Run      string   `yaml:"run"      json:"run"`
	ExitCode int      `yaml:"exitCode" json:"exitCode"`
	Stdout   *Match   `yaml:"stdout"   json:"stdout,omitempty"`
	Timeout  Duration `yaml:"timeout"  json:"timeout"`
}

// Match is a condition on a command's standard output: criterion 1's second
// half, alongside the exit code. Exactly one field is set.
//
// The fields are pointers because an empty string is a meaningful condition:
// `stdout: { equals: "" }` asserts that a command printed nothing.
type Match struct {
	Equals   *string `yaml:"equals"   json:"equals,omitempty"`
	Contains *string `yaml:"contains" json:"contains,omitempty"`
	Matches  *string `yaml:"matches"  json:"matches,omitempty"`
}

// Duration is a time.Duration written the way the manifest writes it ("60s"),
// and rendered the same way in reports so that golden files stay readable.
type Duration time.Duration

// UnmarshalYAML accepts a Go duration string.
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("not a duration: %q", s)
	}
	*d = Duration(v)
	return nil
}

// MarshalJSON renders the duration as a string, not a nanosecond count.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

func (d Duration) String() string { return time.Duration(d).String() }
