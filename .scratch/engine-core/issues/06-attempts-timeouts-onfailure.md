# 06: Attempts, timeouts and `onFailure`

Status: ready-for-agent

Type: AFK

## Parent

[PRD: Engine core — manifest to verified Environment](../PRD.md)

## What to build

The retry and failure-handling half of a Task's lifecycle: `attempts`, the timeout window a retry waits out, `onFailure`, and the ability for a Task to *require* a failing command so that planting a problem can be asserted directly.

## Acceptance criteria

- [ ] `attempts` is the total number of attempts including the first, defaulting to 1
- [ ] After a failed attempt, if attempts remain, the runner waits out the timeout window and starts the next attempt from a clean Probe state
- [ ] When no attempts remain, `onFailure` decides: `fail` fails the build, `ignore` continues to the next Task
- [ ] `exitCode: nonzero` lets a Task require a failing command
- [ ] `exitCode` accepts a single number or a list
- [ ] An ignored failure is recorded in the report and the event stream as ignored, never as success
- [ ] A failed Task fails the build with the failed-Task exit code
- [ ] Attempt counts and retry timing are asserted against the injected clock, with no real sleeping

## Blocked by

- [05: Probes and wait Tasks](./05-probes-and-wait-tasks.md)
