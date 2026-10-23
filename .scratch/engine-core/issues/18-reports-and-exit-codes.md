# 18: Report schema versioning and the seven-outcome exit-code matrix

Status: ready-for-agent

Type: AFK

## Parent

[PRD: Engine core — manifest to verified Environment](../PRD.md)

## What to build

The compatibility guarantee the future CKA Exam Simulator depends on, and the exit-code contract CI depends on.

Apply and Check reports both carry a schema version, and within a manifest API version their fields may only be added — a pinned-shape test makes a removal or a meaning change fail CI rather than quietly break a consumer. Exit codes distinguish all seven outcomes and are assigned in one place.

## Acceptance criteria

- [ ] Apply and Check reports both carry a schema version
- [ ] Within a manifest API version, report fields may only be added; a test pins the current shape so a removal or a changed meaning fails CI
- [ ] Exit codes distinguish all seven outcomes: invalid manifest, missing Capability, provisioning error, failed Task, failed Check, Check that could not run, and interruption
- [ ] Exit codes are assigned in one place and are documented
- [ ] The library and the CLI return the same report content for the same run
- [ ] The CLI's JSON-lines output carries the same event taxonomy the library streams
- [ ] Each of the seven outcomes has a test that provokes it and asserts the code, at seam S4

## Blocked by

- [03: Walking skeleton — `check` and `status`](./03-skeleton-check-status.md)
- [06: Attempts, timeouts and `onFailure`](./06-attempts-timeouts-onfailure.md)
- [15: Capability gating and the host-affecting-command guard](./15-capabilities-and-command-guard.md)
