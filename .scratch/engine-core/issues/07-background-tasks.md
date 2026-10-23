# 07: Background Tasks

Status: ready-for-agent

Type: AFK

## Parent

[PRD: Engine core — manifest to verified Environment](../PRD.md)

## What to build

Background Tasks: a Task that starts a long-running process and is not waited for, so an Environment can include a running load generator or a port-forward. Because nothing waits for the command, criterion 1 does not apply and the Task's Probes decide whether it succeeded.

Each background process is a named handle owned by its Machine and named after its Task, which gives the task runner a stable way to stop it before a retry and makes removal with the Machine automatic.

## Acceptance criteria

- [ ] A background Task starts its command and is not waited for
- [ ] Criterion 1 is rejected at validation for a background Task; its Probes decide success
- [ ] A background Task with no Probes is a validation error
- [ ] The process is a named handle owned by the Machine, named after its Task
- [ ] The process is stopped before its Task re-runs on a retry
- [ ] The process is removed with its Machine, leaving no orphan after `destroy`
- [ ] The background start and stop operations are covered by the conformance suite, so both the `fake` and `container` providers are held to the same contract

## Blocked by

- [05: Probes and wait Tasks](./05-probes-and-wait-tasks.md)
