# 14: The create-only apply state machine and crash recovery

Status: ready-for-agent

Type: AFK

## Parent

[PRD: Engine core — manifest to verified Environment](../PRD.md)

## What to build

The full create-only semantics of `apply`, driven by the Plan hash, plus the incomplete marker and recovery from a killed process.

Because Checks are not part of the Plan, editing them never changes the Plan hash and so never forces a rebuild — an author can refine verification against an Environment that is already built. The incomplete marker is written before the first mutation and cleared only on full success, so a killed process leaves it behind by construction rather than by cleanup.

Every created resource is tagged with its Environment name, so `status` and `destroy` still work when the state directory has been deleted.

## Acceptance criteria

- [ ] With no such Environment, `apply` creates one
- [ ] With a complete Environment of the same Plan hash, `apply` reports up to date, changes nothing, and finishes quickly
- [ ] With a different Plan hash, `apply` refuses and names what changed, unless a recreate is requested
- [ ] A requested recreate destroys and rebuilds
- [ ] Editing only Checks does not change the Plan hash and never forces a rebuild
- [ ] An incomplete marker is written before the first mutation and cleared only on full success
- [ ] An Environment whose last apply failed or was interrupted is reported incomplete and never up to date
- [ ] The next apply after a failure rebuilds from scratch rather than resuming
- [ ] Every created resource is tagged with its Environment name, so `status` and `destroy` work with the state directory deleted
- [ ] A killed process never blocks the next apply or destroy
- [ ] Recovery is asserted at seam S1 by simulating a crash mid-apply against the `fake` provider

## Blocked by

- [03: Walking skeleton — `check` and `status`](./03-skeleton-check-status.md)
