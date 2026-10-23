# 21: End-to-end Docker suite — zero residue, reproducibility, interrupt

Status: ready-for-agent

Type: AFK

## Parent

[PRD: Engine core — manifest to verified Environment](../PRD.md)

## What to build

The seam S5 suite: a small, deliberately high-value set of build-tagged tests against real Docker, proving the things the `fake` provider cannot. It is excluded from the default test invocation, so the everyday test run still needs no Docker.

Engine behaviour is not re-tested here — that belongs at seam S1. This suite exists to prove that real containers really behave as the conformance suite assumes, and that the engine leaves nothing behind.

## Acceptance criteria

- [ ] A build-tagged end-to-end suite runs against real Docker and is excluded from the default test invocation
- [ ] After `destroy`, zero containers, networks and volumes belonging to the Environment remain, asserted by querying Docker directly
- [ ] After a cancelled run, a `destroy` still leaves zero resources
- [ ] A `destroy` followed by an `apply` reproduces the same Plan, the same Machine names and the same IPs
- [ ] An interrupt during a real apply stops within ten seconds
- [ ] A real multi-Machine Environment on two networks applies, checks and destroys cleanly
- [ ] A prerequisite check runs before every apply and reports an unsuitable host clearly, naming what is wrong
- [ ] Engine behaviour is not re-tested here; this suite covers only real-Docker behaviour

## Blocked by

- [10: Networks, multiple Machines, static IPs and Machine IP templates](./10-networks-machines-ips.md)
- [14: The create-only apply state machine and crash recovery](./14-apply-state-machine.md)
- [16: Interruption, backend timeouts and transient-error backoff](./16-interruption-and-backoff.md)
