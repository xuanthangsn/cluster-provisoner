package spec

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Test reports whether stdout satisfies the condition, and if not, why.
//
// Trailing whitespace is trimmed from the output before comparing, as the
// manifest design specifies, so that a command's final newline does not have
// to be written into every condition. Leading whitespace is significant.
func (m *Match) Test(stdout []byte) (bool, string) {
	if m == nil {
		return true, ""
	}
	got := strings.TrimRightFunc(string(stdout), unicode.IsSpace)

	switch {
	case m.Equals != nil:
		if got == *m.Equals {
			return true, ""
		}
		return false, fmt.Sprintf("stdout %q does not equal %q", abbreviate(got), *m.Equals)
	case m.Contains != nil:
		if strings.Contains(got, *m.Contains) {
			return true, ""
		}
		return false, fmt.Sprintf("stdout %q does not contain %q", abbreviate(got), *m.Contains)
	case m.Matches != nil:
		re, err := regexp.Compile(*m.Matches)
		if err != nil {
			// Validation compiles every pattern, so this is unreachable for a
			// validated manifest.
			return false, fmt.Sprintf("pattern %q does not compile: %v", *m.Matches, err)
		}
		if re.MatchString(got) {
			return true, ""
		}
		return false, fmt.Sprintf("stdout %q does not match %q", abbreviate(got), *m.Matches)
	}
	return true, ""
}

// validate checks that exactly one condition is set and that a pattern
// compiles, reporting at path.
func (m *Match) validate(errs *Errors, path string) {
	if m == nil {
		return
	}
	set := 0
	for _, f := range []*string{m.Equals, m.Contains, m.Matches} {
		if f != nil {
			set++
		}
	}
	if set != 1 {
		errs.add(path, fmt.Sprintf("exactly one of equals, contains and matches is required, found %d", set),
			"give one condition; to require several, add a Probe (issue 05)")
		return
	}
	if m.Matches != nil {
		if _, err := regexp.Compile(*m.Matches); err != nil {
			errs.add(path+".matches", fmt.Sprintf("not a valid regular expression: %v", err),
				"patterns use Go's RE2 syntax")
		}
	}
}

// abbreviate keeps a mismatch message readable when output is long.
func abbreviate(s string) string {
	const limit = 200
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}
