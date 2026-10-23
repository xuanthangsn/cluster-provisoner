# Cluster Provisioning Engine

A Go library plus the `simctl` CLI that turns one declarative YAML
`Environment` manifest into a running set of Linux machines, configured by
ordered tasks and verified by optional checks. v1 targets a container backend
(Docker) on Linux. See [prd.md](prd.md) for product intent and
[manifest-layout-design.md](manifest-layout-design.md) for the manifest design.

## Building and testing

Go is installed at `~/.local/go` and is not on `PATH`. Export it first:

```
export PATH="$HOME/.local/go/bin:$PATH"
export GOTOOLCHAIN=local
go test ./... -count=1
```

Golden files are regenerated with `go test ./planner -update`.

The default run needs no Docker. Tests against real Docker — the provider
conformance suite, the out-of-band checks in `provider/container`, and `e2e` —
are behind a build tag:

```
go test -tags docker ./...
```

This machine's Docker daemon also runs the user's own workloads. Never run a
host-wide cleanup (`docker volume prune`, `docker system prune`, and the like);
remove only resources this work created, by ID or by the `dev.sim.environment`
label.

## Agent skills

### Issue tracker

Issues and PRDs live as markdown files under `.scratch/<feature>/` in this
repo. See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical triage roles, unchanged: `needs-triage`, `needs-info`,
`ready-for-agent`, `ready-for-human`, `wontfix`. See
`docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` plus `docs/adr/` at the repo root. See
`docs/agents/domain.md`.
