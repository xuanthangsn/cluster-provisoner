# 13: Image catalog, digest pinning and offline reuse

Status: ready-for-agent

Type: AFK

## Parent

[PRD: Engine core — manifest to verified Environment](../PRD.md)

## What to build

The image catalog, and the separation that keeps planning pure while still pinning by digest. The catalog resolves aliases and OCI references to digests and is the only module that contacts a registry; the planner receives the already-resolved catalog as an input.

The engine downloads images and nothing else — scenarios supply their own manifests and charts as files, so what an Environment installs is exactly what the author provided.

## Acceptance criteria

- [ ] Images are referenced through a catalog of aliases as well as by direct OCI reference
- [ ] The catalog resolves aliases and tags to digests and is the only module that contacts a registry
- [ ] The planner receives the resolved catalog as an input, and so stays pure and network-free
- [ ] The Plan pins every image by digest
- [ ] An unknown alias is rejected, naming the alias and listing the known ones
- [ ] The engine never downloads manifests or charts — only images
- [ ] Repeat builds reuse cached images through a local registry cache
- [ ] After a first successful run, an unchanged Environment applies with no network access
- [ ] Planner tests run against a recorded catalog, with no live registry

## Blocked by

- [01: Walking skeleton — `validate` and `plan`](./01-skeleton-validate-plan.md)
