# 16: Interruption, backend timeouts and transient-error backoff

Status: ready-for-agent

Type: AFK

## Parent

[PRD: Engine core — manifest to verified Environment](../PRD.md)

## What to build

Making a run safe to abandon and resilient to a momentary backend hiccup. Ctrl-C or context cancellation stops work within ten seconds and leaves the incomplete marker in place. Every backend call and command is bounded, so no operation can hang indefinitely. Transient backend errors are retried with backoff, up to three times, and a retry is visible in the event stream.

Tested against the `fake` provider with injectable failures and the injected clock, so cancellation and backoff are asserted deterministically.

## Acceptance criteria

- [ ] Ctrl-C or context cancellation stops work within ten seconds
- [ ] An interrupted apply leaves the incomplete marker in place
- [ ] A dedicated exit code reports interruption
- [ ] Every backend call and every command is bounded by a timeout; no operation can hang indefinitely
- [ ] Transient backend errors are retried with backoff, up to three times, and each retry is visible in the event stream
- [ ] A non-transient error is not retried
- [ ] Cancellation during a Probe loop, during a command, and during Machine creation are each covered
- [ ] Driven against the `fake` provider with injectable failures and the injected clock, with no real sleeping

## Blocked by

- [05: Probes and wait Tasks](./05-probes-and-wait-tasks.md)
