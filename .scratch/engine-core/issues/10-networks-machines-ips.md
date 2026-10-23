# 10: Networks, multiple Machines, static IPs and Machine IP templates

Status: ready-for-agent

Type: AFK

## Parent

[PRD: Engine core — manifest to verified Environment](../PRD.md)

## What to build

The `networks` section, several Machines from one declaration, static and auto-allocated IPs, multi-homed Machines, and Machine IP templating.

Names and IPs are allocated in the Plan, before anything is created, and are stable across a destroy-and-apply cycle — this is the property the parent PRD's reproducibility metric depends on.

## Acceptance criteria

- [ ] A declared `networks` section creates those networks; with no section, one network named `default` is created
- [ ] Subnets auto-allocate as /24 from the documented range, and overlapping subnets are rejected
- [ ] A static IP outside its network's subnet is rejected, and a Machine naming an undeclared network is rejected
- [ ] `names` creates several identical Machines from one declaration
- [ ] A Machine may join several networks and take a per-network address
- [ ] Machine IPs are referenceable from commands, env, Probe commands and transfer paths
- [ ] Allocated names and IPs appear in the Plan before anything is created
- [ ] A destroy followed by an apply reproduces the same names and the same IPs
- [ ] Machines of different Environments do not share a network unless declared
- [ ] Driven at seam S1 against the `fake` provider, with the network operations covered by the conformance suite for both providers

## Blocked by

- [09: Templates from Task output](./09-templates-task-output.md)
