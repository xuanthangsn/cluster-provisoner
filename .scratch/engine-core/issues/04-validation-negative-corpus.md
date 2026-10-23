# 04: Full validation and the 30-manifest negative corpus

Status: ready-for-agent

Type: AFK

## Parent

[PRD: Engine core — manifest to verified Environment](../PRD.md)

## What to build

Every validation rule the manifest layout defines, and the negative corpus that holds them in place. This is the main defence against the risk that the manifest grows faster than its validation, and it is an acceptance criterion of the parent PRD.

Validation stays pure and Docker-free. It can be built in parallel with the provisioning slices.

## Acceptance criteria

- [ ] Names are DNS-1123 labels within their length limits, and the Environment name is capped at 40 characters
- [ ] Machine names are unique within the Environment; Task and Check names are unique within their own section
- [ ] A `needs` entry naming no declared Task is rejected, naming the offending path
- [ ] A cycle in the Task graph is rejected and the cycle's members are named
- [ ] A template reference to a Task not listed in the referring Task's `needs` is rejected
- [ ] A template reference to an undeclared Machine is rejected
- [ ] Type rules are enforced: an `exec` Task has at most one of `run` and `script` and no transfer fields; a `transfer` Task has `from` and `to` and no exec fields; an `exec` Task with no command has at least one Probe
- [ ] A Task, Probe or Check `machine` naming no declared Machine is rejected, and a Probe belonging to a Task with no Machine must name one
- [ ] Relative paths resolve against the manifest's own directory
- [ ] A referenced local file that does not exist, or that lies outside the Environment directory, is rejected
- [ ] At least thirty invalid manifests are in the corpus, each paired with the exact field path its error must name
- [ ] Every error carries its field path, the rule broken and a fix hint
- [ ] All errors are reported at once, in a deterministic order
- [ ] No Docker and no network access required

## Blocked by

- [01: Walking skeleton — `validate` and `plan`](./01-skeleton-validate-plan.md)
