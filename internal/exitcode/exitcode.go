// Package exitcode holds simctl's exit codes.
//
// They live in one place because CI reacts to the kind of failure, not just to
// failure (FR-31), and because a code once published cannot be reused for
// something else.
package exitcode

// Codes implemented in this release.
const (
	// OK means the operation did what was asked.
	OK = 0

	// Usage means simctl was invoked wrongly, or a file could not be read.
	// It is not a statement about the manifest's contents.
	Usage = 1

	// InvalidManifest means the manifest failed validation or planning.
	InvalidManifest = 2

	// ProvisioningError means the backend failed, so a build or a destroy
	// could not be carried out.
	ProvisioningError = 4

	// TaskFailed means a Task's command ran and did not meet its criterion.
	TaskFailed = 5
)

// Reserved, so that later work cannot quietly reuse a number:
//
//	3  MissingCapability  — the backend lacks a Capability the manifest needs (issue 15)
//	6  CheckFailed        — a Check failed (issue 03)
//	7  CheckErrored       — a Check could not run (issue 03)
//	8  Interrupted        — the run was cancelled (issue 16)
//
// Issue 18 implements the full matrix and the test that provokes each code.
var reserved = map[int]bool{3: true, 6: true, 7: true, 8: true}

// Reserved reports whether code is set aside for an operation this release
// does not implement yet.
func Reserved(code int) bool { return reserved[code] }
