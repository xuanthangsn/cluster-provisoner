# 02: Walking skeleton — `apply` and `destroy` a single-Machine Environment

Status: ready-for-human

Type: HITL

## Parent

[PRD: Engine core — manifest to verified Environment](../PRD.md)

## What to build

The `Apply` and `Destroy` operations for the minimal manifest from slice 01, and with them the architectural spine of the engine: the Machine provider interface, an in-memory `fake` provider, the `container` provider, the exported provider conformance suite, the executor, the task runner reduced to criterion 1, the state directory and the typed event stream.

`apply` creates the Machine, starts it, waits for it to be reachable, runs the single Task, and reports the Environment provisioned once the Task has succeeded. `destroy` removes everything it created.

**This slice is HITL.** It fixes four decisions that the remaining nineteen slices inherit: the provider interface's operation set and error vocabulary, the injected clock's shape, the event taxonomy, and the conformance suite's contract. Review these before building on them — retrofitting any of them later is expensive.

### Verification is two-tier, and both tiers are in scope

Asking the provider whether it did its job proves nothing: a provider that always reports an empty list would pass a "machines are gone" check written against its own enumeration. Every claim about what exists has to be observed outside the thing under test.

- **Tier A — the conformance suite**, driven through the provider interface only, run against both `fake` and `container`. It proves the contract is self-consistent and that the two implementations agree. It stays backend-neutral, so it must not query Docker.
- **Tier B — container provider tests**, observing through `docker inspect` and the Docker CLI, never through the provider. This is the only tier that can catch the container provider lying, and several criteria below exist solely because Tier A cannot falsify them: a fake satisfies "files are staged before start" and "a Machine's name is its hostname" by construction.

Tier B lives in the container provider's own test package rather than inside the suite, so the suite stays neutral. An out-of-band observer passed into the suite is the right shape only once a second backend exists; it is not built here.

Deliberately left to later slices: systemd liveness inside a Kubernetes node image (issue 19, since this slice uses plain Linux images where systemd is not PID 1), residue after a cancelled run (issue 21, needs issue 16) and multi-network residue (issue 21, needs issue 10).

## Acceptance criteria

- [x] One Machine provider interface is the only place a backend is implemented, and nothing above it references Docker — enforced by a dependency check in CI, not by convention
- [x] The interface exposes only: declare supplied Capabilities; create and delete a network; create a Machine with its files staged before start, start it and report reachability; delete a Machine with its volumes; run a command with user, env and timeout returning captured output and exit code; start and stop a named background process; copy a file in and out; enumerate an Environment's Machines, networks and volumes
- [x] No container IDs, image-pull semantics or Docker error types leak through the interface
- [x] Nothing Kubernetes-shaped appears in the interface
- [x] An in-memory `fake` provider ships in the repository and passes the conformance suite
- [x] The `container` provider passes the same conformance suite, behind a build tag requiring Docker
- [x] The conformance suite is an exported package, runnable against a backend developed outside this repository
- [x] Every operation that waits takes an injected clock; no module above the provider reads the system clock
- [x] A Task succeeds on criterion 1 alone: its exit code and optional stdout condition match
- [x] A Machine's name is its hostname
- [x] Tasks run one at a time, and the order is a property of the Plan rather than of the run
- [x] A typed event stream reports Task started, Task finished, command output, warnings and errors; the CLI renders those same events as JSON lines, with no second event path
- [x] The state directory holds the Plan and the run log, with owner-only permissions on anything secret, set at creation
- [x] `destroy` removes the Machine, its volumes and the state files, and succeeds when nothing exists
- [x] Every backend call and every command is bounded by a timeout
- [x] Apply is driven through the library API at seam S1 against the `fake` provider, asserting on the returned report and the event stream — never on the fake's record of calls
- [x] Unit tests above the provider run with Docker stopped and finish well inside the sixty-second budget

Observed outside the provider, against real Docker (Tier B):

- [x] A Machine's existence, running state and configured hostname are asserted with `docker inspect`, and its kernel hostname is read from inside the Machine — never through the provider
- [x] Every container, network and volume carries a label naming its Environment, and a label-filtered Docker query returns exactly the expected set
- [x] Files staged before start are proven by reading the file back out of the container while it is still in the `created` state, before Start is called — asserting the file exists after start would not prove it was there before
- [x] Reachability has a falsifiable meaning: once Start returns, a command runs and exits 0 with no retry by the test
- [x] A command's exit code, stdout and stderr are captured exactly, including output with no trailing newline and output larger than a pipe buffer
- [x] A Task's side effect is read back out of band, and on a two-Machine fixture is absent on the Machine the Task did not name, so "ran on the wrong Machine" cannot pass
- [x] Sequential execution is proven from the filesystem: three Tasks appending to one file leave exactly the expected sequence, rather than the report's own ordering being trusted
- [x] After `destroy`, a whole-host set difference of containers, networks and volumes is empty — not only a label-filtered query, which is blind to a leak of an unlabelled resource
- [x] `destroy` is idempotent against real Docker: running it twice succeeds and leaves nothing
- [x] Every Docker-level test is build-tagged, so the default test invocation still needs no Docker

## Blocked by

- [01: Walking skeleton — `validate` and `plan`](./01-skeleton-validate-plan.md)

## Implementation notes

Implemented, and awaiting the HITL review this issue calls for. The status stays
`ready-for-human` until the four decisions below have been reviewed.

### Evidence

| Run | Result |
| --- | --- |
| `go test ./...` with Docker unreachable | 174 passing, 0.3 s |
| `go test -tags docker ./...`, packages in parallel | 215 of 215, three consecutive runs, host identical before and after |
| `-race`, `go vet` (both tag sets), `gofmt` | clean |

Coverage: spec 84.5%, planner 87.0%, executor 88.7% (through the library API),
taskrunner 91.9%, container provider 78.1% (with Docker).

The tests themselves were checked by planting bugs, each caught with file and
line: a `time.Now()` and a Docker string above the provider; a wrong hostname
(caught by Tier B at two levels, and by Tier A); volumes leaked on delete; and
networks created without labels — the leak a label-filtered query cannot see,
caught only by the whole-host comparison.

### The four decisions for review

1. **The provider interface** — twelve methods, addressed by Environment and
   Machine name, with four error kinds (`ErrNotFound`, `ErrAlreadyExists`,
   `ErrNotRunning`, `ErrUnavailable`). Deletes and stops are idempotent.
   `StartMachine` returns only once exec works, which is what makes
   reachability falsifiable. A command that runs is never an error, whatever
   its exit code; a timeout is reported as `TimedOut` on the result.
2. **The injected clock** — `Now()` and `After(d)`, with a `Fake` that moves
   only on `Advance`. Nothing above the provider waits yet; `After` is in the
   shape now so that Probes (issue 05) do not change the interface.
3. **The event taxonomy** — thirteen events, one path. The library streams
   them, simctl renders the same values as JSON lines, and the run log *is*
   that JSON-lines stream. With `--json`, simctl ends the stream with one
   `apply.report` or `destroy.report` line, which is the report, not an event.
4. **The conformance contract** — sixteen cases, backend-neutral, run against
   the fake and against Docker.

### Decisions made along the way

- **The container provider speaks the Engine API over the Unix socket, not the
  docker CLI.** Measured on this host: `docker exec` exits 1 both when a
  command exits 1 and when the daemon refuses the call, and a command can print
  the daemon's own error text, so the CLI cannot report exit codes exactly. The
  API reports them from exec-inspect. Talking HTTP directly also avoids the
  Docker SDK's dependency tree; the module still depends only on `yaml.v3`.
- **Resources are named `<environment>_<name>`**: the separator cannot occur in
  a DNS label, so two Environments can never collide. Every resource carries
  `dev.sim.managed`, `dev.sim.environment` and `dev.sim.machine` or
  `dev.sim.network` labels — reverse-DNS of the `sim.dev` placeholder, so they
  change with issue 20.
- **The provider refuses to delete a container or network that lacks its
  Environment's label**, because the host runs unrelated workloads whose names
  could collide.
- **A Task's command runs as `bash -c`**, as the manifest design specifies.
- **Apply refuses an Environment that already exists** and leaves a failed
  build in place for inspection. Recognising an unchanged Environment, and
  recreating on request, is issue 14.
- **An optional stdout condition** (`equals`, `contains`, `matches`) was added
  to Tasks and Checks, because criterion 1 requires one.
- **The whole-host comparison ignores resources labelled for a different
  Environment.** Test packages run in parallel against one daemon, and the
  first full run failed on the conformance suite's own resources. Anything
  unlabelled still counts in both directions.

### Known limits, each owned by a later issue

- A Machine runs `tail -f /dev/null` under Docker's init in place of its
  image's command. That keeps plain images alive but will not boot systemd in a
  Kubernetes node image — issue 19.
- A command that outlives its timeout is reported as timed out but keeps
  running inside the Machine — issues 05 and 06.
- Background processes are tracked by pid file — issue 07 may move them to
  systemd units.
- The planner gives every Environment `172.30.0.0/24`, so only one can exist at
  a time, and the e2e tests build one at a time — issue 10.
- Tier B was run on one host: Docker 29.4.0, cgroup v2, overlayfs.

### Gap found in the breakdown

No issue adds the manifest's `user` and `env` fields for Tasks, although the
provider supports both and the design lists them. They are not needed until a
scenario runs a command as another user.
