# 12: Machine resource limits and local build contexts

Status: ready-for-agent

Type: AFK

## Parent

[PRD: Engine core — manifest to verified Environment](../PRD.md)

## What to build

`cpus` and `memory` limits per Machine, so a large Environment stays within the host's resources, and creating a Machine from a local build context with build arguments instead of a pre-built image.

A built image is built once on first use and reused afterwards. Both limits and build support are declared as provider Capabilities, so a backend lacking either is caught by the planner rather than at run time.

## Acceptance criteria

- [ ] `cpus` and `memory` are applied as limits
- [ ] It is documented that on the container backend these do not change kubelet-reported capacity
- [ ] A Machine may be created from a local build context with build arguments instead of an image
- [ ] Exactly one of `image` and `build` is required
- [ ] A build context that does not exist is rejected at validation
- [ ] An image is built once on first use and reused afterwards; a second apply does not rebuild an unchanged context
- [ ] Limit and build support are declared as provider Capabilities, so a backend lacking them is caught by the planner

## Blocked by

- [02: Walking skeleton — `apply` and `destroy`](./02-skeleton-apply-destroy.md)
