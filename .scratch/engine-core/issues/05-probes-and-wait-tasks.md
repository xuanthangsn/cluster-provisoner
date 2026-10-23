# 05: Probes and wait Tasks

Status: ready-for-agent

Type: AFK

## Parent

[PRD: Engine core — manifest to verified Environment](../PRD.md)

## What to build

Probes, and the second success criterion they provide. A Task succeeds only when its own result matches *and* every Probe's latest result passes at the same moment, within the Task's timeout. This is what lets an author prove a planted breakage actually took effect.

Also the wait Task: an `exec` Task with no command and at least one Probe, so a manifest can wait for a state such as a node becoming NotReady rather than guessing a sleep.

All timing is measured against the injected clock, so tests assert exact Probe counts and intervals without sleeping.

## Acceptance criteria

- [ ] A Probe re-runs at its interval, defaulting to 2s, and continues re-running after it has passed; only its latest result counts
- [ ] A Task succeeds only when criterion 1 holds and every Probe's latest result passes at the same moment, within the timeout
- [ ] A Task whose command succeeds but whose Probe never passes fails
- [ ] A Task with no command and at least one Probe is a valid wait Task
- [ ] A Probe inherits the Task's env and defaults to the Task's Machine; a Probe on a Machine-less Task must name one
- [ ] Probe `exitCode` and `stdout` conditions — `equals`, `contains` and `matches` — all work, with trailing whitespace trimmed
- [ ] A Task still running when its timeout expires fails
- [ ] Probe results appear in the event stream and in the apply report, including the output that finally matched
- [ ] All timing is driven by the injected clock; tests assert exact Probe counts and intervals with no real sleeping

## Blocked by

- [02: Walking skeleton — `apply` and `destroy`](./02-skeleton-apply-destroy.md)
