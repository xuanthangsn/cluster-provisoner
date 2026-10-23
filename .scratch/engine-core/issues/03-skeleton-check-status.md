# 03: Walking skeleton — `check` and `status`

Status: ready-for-agent

Type: AFK

## Parent

[PRD: Engine core — manifest to verified Environment](../PRD.md)

## What to build

The check engine and the `Status` operation, completing the six-operation lifecycle for the minimal manifest.

Checks run only when asked, never as part of `apply`, and never change the Environment. They run one at a time in declaration order, each independent of the others, each ending pass, fail or error with its duration and the output observed. `Status` reports what exists and how the last run ended.

## Acceptance criteria

- [ ] `apply` never runs Checks; its report states how many Checks are defined and that they must be run separately
- [ ] `simctl check` runs the Checks and changes nothing — a `status` taken before and after is identical
- [ ] A Check accepts the same fields as an `exec` Task except `type`, `background` and `needs`; using `needs` or `background` on a Check is a validation error
- [ ] Checks run one at a time in declaration order and cannot read each other's output
- [ ] Each Check result records pass, fail or error, its duration, and the output observed
- [ ] A Check that could not run is reported as error rather than fail, and is distinguishable in both the report and the exit code
- [ ] One Check failing does not prevent the remaining Checks from running
- [ ] The Check run passes only when every Check passes
- [ ] `status` reports the Environment, its Machines and the last run's result
- [ ] The Check report is versioned JSON, and the library and the CLI return the same report content
- [ ] Driven at seam S1 against the `fake` provider, asserting on reports rather than on provider calls

## Blocked by

- [02: Walking skeleton — `apply` and `destroy`](./02-skeleton-apply-destroy.md)
