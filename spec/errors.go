package spec

import (
	"fmt"
	"sort"
	"strings"
)

// Error is one problem with a manifest.
//
// Every error carries all three parts (FR-3, NFR-13): where the problem is,
// which rule it broke, and what to do about it. A validation error without a
// Hint is a bug in this package, not a terse error.
type Error struct {
	// Path is the manifest field path, such as "tasks.join-node02.needs[0]".
	// It is empty only for problems with the document as a whole.
	Path string `json:"path,omitempty"`
	// Rule is what was expected, phrased so it reads after the path.
	Rule string `json:"rule"`
	// Hint suggests a fix.
	Hint string `json:"hint"`
}

func (e Error) Error() string {
	if e.Path == "" {
		return e.Rule
	}
	return e.Path + ": " + e.Rule
}

// Errors is every problem found in one pass, in a deterministic order.
//
// Validation always reports all of them rather than stopping at the first, so
// that an author can fix a whole file in one pass (FR-3).
type Errors []Error

func (es Errors) Error() string {
	switch len(es) {
	case 0:
		return "no errors"
	case 1:
		return es[0].Error()
	}
	parts := make([]string, 0, len(es))
	for _, e := range es {
		parts = append(parts, e.Error())
	}
	return fmt.Sprintf("%d problems: %s", len(es), strings.Join(parts, "; "))
}

// Err returns es as an error, or nil when there are none. Useful because a
// non-nil Errors of length zero is still a non-nil error interface value.
func (es Errors) Err() error {
	if len(es) == 0 {
		return nil
	}
	return es
}

// add appends one problem.
func (es *Errors) add(path, rule, hint string) {
	*es = append(*es, Error{Path: path, Rule: rule, Hint: hint})
}

// Sorted returns the errors in a deterministic order: by path, then by rule.
//
// Sorting by path groups everything wrong with one field together. It is a
// plain string sort, so numeric path indices order lexically; that is
// deterministic, which is what callers and tests rely on.
func (es Errors) Sorted() Errors {
	out := make(Errors, len(es))
	copy(out, es)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Rule < out[j].Rule
	})
	return out
}
