# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

This repo is **single-context**: one vocabulary, one `CONTEXT.md`, one ADR directory. The CKA Exam Simulator is a separate product with its own PRD and its own repo, so its language does not belong here.

## Before exploring, read these

- **`CONTEXT.md`** at the repo root — the project's glossary and domain language.
- **`docs/adr/`** — read ADRs that touch the area you're about to work in.

If either doesn't exist, **proceed silently**. Don't flag their absence; don't suggest creating them upfront. The producer skill (`/grill-with-docs`) creates them lazily when terms or decisions actually get resolved.

## File structure

```
/
├── CONTEXT.md
├── docs/
│   ├── adr/
│   │   ├── 0001-<decision>.md
│   │   └── 0002-<decision>.md
│   └── agents/          ← this file, plus issue-tracker.md and triage-labels.md
├── prd.md
└── manifest-layout-design.md
```

## Use the glossary's vocabulary

When your output names a domain concept (in an issue title, a refactor proposal, a hypothesis, a test name), use the term as defined in `CONTEXT.md`. Don't drift to synonyms the glossary explicitly avoids.

Until `CONTEXT.md` exists, section 5 ("Key concepts") of `prd.md` is the authoritative vocabulary, and it is what `CONTEXT.md` should be seeded from: Environment, Machine, Machine provider, Task, Probe, Check, Capability, Plan. Note in particular that **Cluster is not an engine concept** — a Kubernetes cluster exists only because tasks ran kubeadm, and the engine never names, reads or validates clusters. Don't introduce cluster-level language into engine code or issues.

If the concept you need isn't in the glossary yet, that's a signal — either you're inventing language the project doesn't use (reconsider) or there's a real gap (note it for `/grill-with-docs`).

## Flag ADR conflicts

If your output contradicts an existing ADR, surface it explicitly rather than silently overriding:

> _Contradicts ADR-0007 (event-sourced orders) — but worth reopening because…_
