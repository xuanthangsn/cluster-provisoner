# 09: Templates from Task output

Status: ready-for-agent

Type: AFK

## Parent

[PRD: Engine core — manifest to verified Environment](../PRD.md)

## What to build

Templating from earlier Tasks' results, so that a join command produced on one Machine can run on another without the author copying it by hand. A Task's command, env, Probe commands and transfer paths may reference an earlier Task's stdout and exit code.

References resolve immediately before the Task runs. Validation already restricts references to Tasks named in the referring Task's `needs`, so resolution can never fail at run time for a reference reason.

## Acceptance criteria

- [ ] A Task's command, env, Probe commands and transfer paths may reference an earlier Task's stdout and exit code
- [ ] References resolve immediately before the Task runs
- [ ] Only Tasks named in the referring Task's `needs` may be referenced; anything else is a validation error
- [ ] Trailing whitespace is trimmed consistently with stdout matching
- [ ] A multi-line stdout substitutes correctly into a command
- [ ] Resolved values appear in the run log, so a run can be reconstructed after the fact
- [ ] An unresolvable reference at run time is an engine bug, not a user error — covered by a test asserting validation catches it first

## Blocked by

- [02: Walking skeleton — `apply` and `destroy`](./02-skeleton-apply-destroy.md)
