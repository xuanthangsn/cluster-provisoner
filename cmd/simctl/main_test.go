package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sim.dev/engine"
	"sim.dev/engine/internal/exitcode"
	"sim.dev/engine/planner"
)

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

func write(t *testing.T, manifest string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "environment.yaml")
	if err := os.WriteFile(path, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func invoke(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestExitCodes(t *testing.T) {
	valid := write(t, minimal)
	invalid := write(t, "kind: Environment\n")
	absent := filepath.Join(t.TempDir(), "absent.yaml")

	cases := []struct {
		name string
		args []string
		want int
	}{
		{"a valid manifest validates", []string{"validate", "-f", valid}, exitcode.OK},
		{"a valid manifest plans", []string{"plan", "-f", valid}, exitcode.OK},
		{"an invalid manifest fails validate", []string{"validate", "-f", invalid}, exitcode.InvalidManifest},
		{"an invalid manifest fails plan", []string{"plan", "-f", invalid}, exitcode.InvalidManifest},
		{"a missing file is a usage problem", []string{"validate", "-f", absent}, exitcode.Usage},
		{"no -f is a usage problem", []string{"validate"}, exitcode.Usage},
		{"an unknown command is a usage problem", []string{"provision"}, exitcode.Usage},
		{"no arguments is a usage problem", nil, exitcode.Usage},
		{"help succeeds", []string{"help"}, exitcode.OK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, stdout, stderr := invoke(t, tc.args...)
			if got != tc.want {
				t.Errorf("exit code = %d, want %d\nstdout: %s\nstderr: %s", got, tc.want, stdout, stderr)
			}
		})
	}
}

// TestValidateJSONIsExactlyTheLibraryReport is how "simctl adds no behaviour
// of its own" is held in place: the machine-readable output is the library's
// report and nothing else.
func TestValidateJSONIsExactlyTheLibraryReport(t *testing.T) {
	path := write(t, minimal)

	code, stdout, stderr := invoke(t, "validate", "-f", path, "--json")
	if code != exitcode.OK {
		t.Fatalf("exit code = %d; stderr: %s", code, stderr)
	}

	report, err := engine.Validate(path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')

	if stdout != string(want) {
		t.Errorf("simctl printed something other than the library report\n got:\n%s\nwant:\n%s", stdout, want)
	}
}

func TestPlanJSONIsExactlyTheLibraryReport(t *testing.T) {
	path := write(t, minimal)

	code, stdout, stderr := invoke(t, "plan", "-f", path, "--json")
	if code != exitcode.OK {
		t.Fatalf("exit code = %d; stderr: %s", code, stderr)
	}

	report, err := engine.Plan(path, planner.Passthrough{})
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')

	if stdout != string(want) {
		t.Errorf("simctl printed something other than the library report\n got:\n%s\nwant:\n%s", stdout, want)
	}
}

func TestInvalidManifestStillPrintsItsReportAsJSON(t *testing.T) {
	path := write(t, "kind: Environment\n")

	code, stdout, _ := invoke(t, "validate", "-f", path, "--json")
	if code != exitcode.InvalidManifest {
		t.Errorf("exit code = %d, want %d", code, exitcode.InvalidManifest)
	}

	var report engine.ValidateReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("output is not the report: %v\n%s", err, stdout)
	}
	if report.Valid {
		t.Error("report says the manifest is valid")
	}
	if len(report.Errors) == 0 {
		t.Error("report carries no problems")
	}
}

func TestHumanOutputNamesEveryProblemWithItsPathAndHint(t *testing.T) {
	path := write(t, "apiVersion: sim.dev/v1alpha2\nkind: Environment\nname: x\nmachines: [{name: m}]\n")

	code, stdout, _ := invoke(t, "validate", "-f", path)
	if code != exitcode.InvalidManifest {
		t.Fatalf("exit code = %d, want %d", code, exitcode.InvalidManifest)
	}
	for _, want := range []string{"machines[0].image", "required", "hint:"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output does not mention %q\ngot:\n%s", want, stdout)
		}
	}
}

func TestHumanValidateOutputSummarisesAValidManifest(t *testing.T) {
	_, stdout, _ := invoke(t, "validate", "-f", write(t, minimal))

	for _, want := range []string{"skeleton", "1 machine", "1 task", "1 check"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output does not mention %q\ngot:\n%s", want, stdout)
		}
	}
}

func TestHumanPlanOutputShowsNamesAddressesAndOrder(t *testing.T) {
	_, stdout, _ := invoke(t, "plan", "-f", write(t, minimal))

	for _, want := range []string{"skeleton", "hash ", "node01", "172.30.0.", "greet", "Nothing has been created"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output does not mention %q\ngot:\n%s", want, stdout)
		}
	}
}

func TestProblemsAreNotPrintedToStderrWhenTheyAreTheAnswer(t *testing.T) {
	// A manifest's problems are the requested output, so they go to stdout;
	// stderr is for simctl failing to do its job.
	path := write(t, "kind: Environment\n")

	_, stdout, stderr := invoke(t, "validate", "-f", path)
	if stdout == "" {
		t.Error("problems were not printed to stdout")
	}
	if stderr != "" {
		t.Errorf("stderr should be empty, got: %s", stderr)
	}
}
