# 17: `status` detail, ad-hoc exec and run logs

Status: ready-for-agent

Type: AFK

## Parent

[PRD: Engine core — manifest to verified Environment](../PRD.md)

## What to build

The operator-facing surface: listing Environments with real detail, running a command on a Machine without writing a Task, and reading the stored log of any past run.

## Acceptance criteria

- [ ] Environments are listable with their Machines, the last Task reached and the last run's result
- [ ] An ad-hoc command can be run on a Machine without writing a Task
- [ ] An ad-hoc command does not alter the Environment's recorded state or its Plan hash
- [ ] The stored log of any run is readable after the process has exited
- [ ] Every command and its output is kept in the run log
- [ ] Logs are structured, with a debug level that shows full backend and command output
- [ ] Secrets in the state directory are owner-only, with permissions set at creation rather than fixed afterwards

## Blocked by

- [03: Walking skeleton — `check` and `status`](./03-skeleton-check-status.md)
