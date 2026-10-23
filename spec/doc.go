// Package spec decodes, defaults and validates an Environment manifest.
//
// Every exported function here is pure: no Docker, no network, no clock, no
// filesystem. That is deliberate — spec and planner are the two seams the
// engine is tested through below the library API, and keeping them pure is what
// lets the manifest be validated on a host that cannot run it (FR-3).
//
// Scope note: this package currently implements the validation rules needed to
// decode, default and plan a minimal Environment. The full rule set and the
// negative corpus that pins it down are issue 04.
package spec
