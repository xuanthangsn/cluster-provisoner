# 15: Capability gating and the host-affecting-command guard

Status: ready-for-agent

Type: AFK

## Parent

[PRD: Engine core — manifest to verified Environment](../PRD.md)

## What to build

The Capability model and the host-affecting-command denylist — both enforced at plan time, so an author learns they need the VM backend in under a second rather than halfway through a build.

Providers declare the Capabilities they supply; the planner rejects a manifest needing an absent one, naming both the field and the Capability. `backend: vm` is a recognised reserved value rejected with a clear message rather than an unknown-value error.

The command guard is documented as an accident guard, not isolation. Real isolation arrives with the VM backend, and the rejection message should say so.

## Acceptance criteria

- [ ] Providers declare the Capabilities they supply
- [ ] The planner rejects a manifest needing an absent Capability, naming both the field and the Capability
- [ ] `backend: vm` is a recognised reserved value rejected with a clear message, not an unknown-value error
- [ ] Known host-affecting commands are rejected at plan time in Tasks, Probes and Checks alike
- [ ] The documented denylist covers at least `swapon`, `swapoff`, `modprobe`, `date -s`, `sysctl -w kernel.*` and `hwclock`
- [ ] The rejection message states that this is an accident guard rather than isolation, and that real isolation arrives with the VM backend
- [ ] Rejection happens in under a second, with no Docker and nothing created
- [ ] A dedicated exit code distinguishes a missing Capability from an invalid manifest

## Blocked by

- [01: Walking skeleton — `validate` and `plan`](./01-skeleton-validate-plan.md)
