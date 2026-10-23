# 01: Walking skeleton — `validate` and `plan` a minimal Environment

Status: done

Type: AFK

## Parent

[PRD: Engine core — manifest to verified Environment](../PRD.md)

## What to build

The `Validate` and `Plan` operations on the library, and the `simctl validate` and `simctl plan` commands over them, for the smallest useful manifest: a header, one `machines` entry with a name and an image, one `exec` Task with a command, and one Check. No `networks` section, no `files`, no Probes.

Both operations are pure and must work with Docker stopped. `Validate` decodes strictly, applies documented defaults, and either returns a defaulted Environment the author can inspect or a sorted list of field-path errors. `Plan` turns that Environment into a Plan carrying the Machine name, its allocated IP, the single Task in order, and a Plan hash.

This slice establishes conventions every later slice inherits: the shape of a validation error (field path, rule broken, fix hint), the versioned report envelope, the CLI-as-thin-adapter pattern, and table-driven plus golden-file testing at seam S3.

## Acceptance criteria

- [x] The library exposes `Validate` and `Plan`; `simctl validate` and `simctl plan` call them and add no behaviour of their own
- [x] A minimal manifest validates and plans with Docker stopped and with no network access
- [x] An unknown field is an error naming that field's path
- [x] An unsupported `apiVersion` is rejected and the supported versions are named
- [x] The fully defaulted Environment is inspectable, with `backend` defaulting to container, Task `type` to exec, `exitCode` to 0, `timeout` to 60s and `attempts` to 1
- [x] The Plan lists the Machine name, its allocated IP and the Task in order, and carries a Plan hash
- [x] Planning the same manifest twice yields a byte-identical Plan, asserted against a golden file with a regeneration flag
- [x] Planning has no side effects: nothing is created and no state directory is written
- [x] Every validation error carries its field path, the rule broken and a hint at the fix
- [x] All validation errors are reported at once, in a deterministic order
- [x] Exit codes distinguish success from an invalid manifest
- [x] Both reports carry a schema version
- [x] `spec` and `planner` are tested as pure functions at seam S3: table-driven, no Docker, no network, no clock

## Blocked by

None - can start immediately.

## Implementation notes

Done. `go test ./... -count=1` passes: 84 tests, 0 failures, 0.12 s. Coverage:
spec 83.1%, planner 87.0%, engine 87.0%, cmd/simctl 88.4% — spec and planner
are both over the 80% target NFR-10 sets for them.

Shape delivered:

- `spec` — strict decode, defaults, validation. Pure.
- `planner` — Environment + Catalog to Plan, with the hash. Pure.
- `engine` — the library surface; Validate and Plan only.
- `internal/exitcode` — the codes, with 3 to 8 reserved and annotated.
- `cmd/simctl` — validate and plan.

Decisions worth knowing before the next slice:

- **Declaration order is preserved** through a generic ordered-map
  (`spec.Section[T]`), because a Go map would lose it and Task order is derived
  from it. This shaped the manifest types, so it was cheaper to settle now.
- **Unknown fields are found by a reflection walk over the manifest types**, not
  by yaml's `KnownFields`, which reports a bare field name and a line where
  FR-3 needs a field path. The walk also builds a line-to-path index so a
  wrongly typed value is reported at its path too.
- **Validation is skipped when a value could not be decoded at all**, since
  rules applied to fields the decoder could not populate point at the wrong
  thing. Unknown fields do not have that problem and are reported alongside
  validation.
- **Two rules from issue 04 are enforced here**: a `needs` entry must name a
  declared Task, and the graph must have no cycles. The planner cannot order
  Tasks safely without them. Issue 04 still owns the other rules and the
  30-manifest corpus.
- **`spec.Errors.Sorted()` is exported** so the planner can return problems in
  the same deterministic order as validation, and the CLI renders both the same
  way.
- The module path is `sim.dev/engine`, mirroring the `sim.dev` placeholder in
  the manifest's `apiVersion`. Both change together when issue 20 settles the
  product name and API group.

Deliberately not built, to keep the slice thin: `names`, networks, static IPs,
limits and build contexts; `files`; Probes, retries, background, transfers and
templates; the image catalog's digest pinning (a `Passthrough` catalog stands
in behind the real `Catalog` seam); Capability gating.
