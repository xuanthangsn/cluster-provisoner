# 11: The `files` section and Machine tags

Status: ready-for-agent

Type: AFK

## Parent

[PRD: Engine core — manifest to verified Environment](../PRD.md)

## What to build

The `files` section and the Machine `tags` that target it. Declared files are written into a Machine *before it starts*, so a service reads the configuration the author intended on its very first start — that ordering is the whole point of the section and is already part of the provider contract.

## Acceptance criteria

- [ ] Declared files are written into a Machine before it starts
- [ ] Inline `content` and a local `source` path both work, and exactly one of them is required
- [ ] `mode` and `owner` are applied, defaulting to 644 and root
- [ ] `append` adds to an existing file instead of replacing it
- [ ] Parent directories are created
- [ ] `on` defaults to all Machines, and accepts Machine names and tag selectors
- [ ] Machine `tags` are free labels usable as `on` targets
- [ ] An `on` entry resolving to no declared Machine or tag is rejected at validation
- [ ] Files-before-start is asserted through the conformance suite, so both providers are held to it

## Blocked by

- [10: Networks, multiple Machines, static IPs and Machine IP templates](./10-networks-machines-ips.md)
