# 08: Transfer Tasks

Status: ready-for-agent

Type: AFK

## Parent

[PRD: Engine core — manifest to verified Environment](../PRD.md)

## What to build

The `transfer` Task type: copying one file between a Machine path, a local file relative to the manifest directory, and a path in the Environment's state directory on the host. This is how a kubeconfig reaches somewhere the author can use it.

A transfer succeeds when the file exists at its destination with the requested mode; criterion 1 does not apply, though Probes may add conditions. Files delivered to the host are listable, and that listing — not a path the implementation happened to choose — is how tests verify a transfer.

## Acceptance criteria

- [ ] A transfer copies one file between a Machine path, a local file relative to the manifest directory, and a state-directory path
- [ ] `mode` and `owner` are applied, defaulting to the source mode and root
- [ ] `owner` on a state-directory destination is rejected rather than silently ignored
- [ ] A transfer succeeds when the file exists at its destination with the requested mode; criterion 1 does not apply
- [ ] Probes may add conditions to a transfer
- [ ] Files delivered to the host are listable through both the library and the CLI
- [ ] A transfer endpoint naming an undeclared Machine is rejected at validation
- [ ] Transfers are verified through the listing operation, not by inspecting a chosen path

## Blocked by

- [02: Walking skeleton — `apply` and `destroy`](./02-skeleton-apply-destroy.md)
