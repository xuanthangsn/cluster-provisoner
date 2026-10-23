package spec_test

import (
	"strings"
	"testing"

	"sim.dev/engine/spec"
)

func withStdout(condition string) string {
	return `
apiVersion: sim.dev/v1alpha2
kind: Environment
name: x
machines: [{ name: m, image: i }]
tasks:
  t: { machine: m, run: "true", stdout: ` + condition + ` }
checks:
  c: { machine: m, run: "true", stdout: ` + condition + ` }
`
}

func TestStdoutConditionValidation(t *testing.T) {
	cases := []struct {
		condition string
		wantPaths []string
	}{
		{`{ equals: "x" }`, nil},
		{`{ equals: "" }`, nil}, // asserting a command printed nothing is meaningful
		{`{ contains: "x" }`, nil},
		{`{ matches: "^(False|Unknown)$" }`, nil},
		{`{}`, []string{"checks.c.stdout", "tasks.t.stdout"}},
		{`{ equals: "a", contains: "b" }`, []string{"checks.c.stdout", "tasks.t.stdout"}},
		{`{ matches: "(unclosed" }`, []string{"checks.c.stdout.matches", "tasks.t.stdout.matches"}},
		{`{ equal: "typo" }`, []string{"checks.c.stdout", "checks.c.stdout.equal", "tasks.t.stdout", "tasks.t.stdout.equal"}},
	}
	for _, tc := range cases {
		t.Run(tc.condition, func(t *testing.T) {
			_, errs := spec.Parse([]byte(withStdout(tc.condition)))
			if got := strings.Join(paths(errs), ","); got != strings.Join(tc.wantPaths, ",") {
				t.Errorf("paths = %q, want %q\nerrors: %v", got, strings.Join(tc.wantPaths, ","), errs)
			}
		})
	}
}

func TestStdoutConditionTest(t *testing.T) {
	s := func(v string) *string { return &v }
	cases := []struct {
		name   string
		match  *spec.Match
		output string
		want   bool
	}{
		{"no condition always holds", nil, "anything", true},
		{"equals ignores trailing whitespace", &spec.Match{Equals: s("ready")}, "ready\n\n  \t", true},
		{"equals keeps leading whitespace", &spec.Match{Equals: s("ready")}, "  ready", false},
		{"empty equals holds for no output", &spec.Match{Equals: s("")}, "\n", true},
		{"empty equals fails for output", &spec.Match{Equals: s("")}, "x", false},
		{"contains", &spec.Match{Contains: s("Ready")}, "node01 Ready", true},
		{"contains matches inside a longer word", &spec.Match{Contains: s("Ready")}, "node01 NotReady", true},
		{"contains, case matters", &spec.Match{Contains: s("ready")}, "Ready", false},
		{"matches", &spec.Match{Matches: s(`^(False|Unknown)$`)}, "Unknown\n", true},
		{"matches, mismatched", &spec.Match{Matches: s(`^(False|Unknown)$`)}, "True", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, why := tc.match.Test([]byte(tc.output))
			if ok != tc.want {
				t.Errorf("Test(%q) = %v (%s), want %v", tc.output, ok, why, tc.want)
			}
			if !ok && why == "" {
				t.Error("a failing condition gave no reason")
			}
		})
	}
}

func TestAMismatchOnLongOutputStaysReadable(t *testing.T) {
	want := "short"
	_, why := (&spec.Match{Equals: &want}).Test([]byte(strings.Repeat("x", 10_000)))
	if len(why) > 400 {
		t.Errorf("the reason is %d bytes; long output should be abbreviated", len(why))
	}
}
