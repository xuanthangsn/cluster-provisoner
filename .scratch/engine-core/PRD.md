  # PRD: Engine core — manifest to verified Environment

  Status: ready-for-agent

  Scope: FR-1 through FR-31 of [prd.md](../../prd.md). Shipped Kubernetes content
  (FR-32, FR-33, FR-34 and the 15-example catalog) is out of scope and belongs to a
  separate PRD.

  Source material: [prd.md](../../prd.md) (product intent) and
  [manifest-layout-design.md](../../manifest-layout-design.md) (manifest layout,
  v1alpha2). Vocabulary follows `prd.md` section 5 — see
  [docs/agents/domain.md](../../docs/agents/domain.md).

  ## Problem Statement

  A scenario author wants to describe a Linux environment once — machines,
  networks, files and an ordered set of tasks — and have it built the same way
  every time, with proof that it reached the state they intended. Today they have
  no way to do that locally.

  Every available option breaks the loop somewhere. A kind config plus a shell
  script creates machines quickly, but everything after creation is hand-run, so
  the end state lives nowhere and cannot be rebuilt or verified. Hand-built VMs
  give full fidelity and then leave all configuration and all deliberate breakage
  to manual work, slowly. Hosted practice platforms offer ready-made scenarios the
  author cannot author, cannot run offline, and cannot build a product on.

  The author's pain is sharpest when the intended end state is *broken on
  purpose*. They need node02's kubelet to fail for a specific, chosen reason, and
  they need to know that it failed for that reason and not by accident — before
  they hand the environment to anyone. Nothing today tells them whether a planted
  problem actually took effect.

  Secondarily, an engine developer wants to add a new Machine provider later
  without rewriting the layers above it, and wants to test those layers without
  Docker running. A future consumer — the CKA Exam Simulator — wants to embed this
  as a library, follow a build as it happens, grade an answer with the
  Environment's own Checks, and remove everything afterwards.

  ## Solution

  A Go library, with `simctl` as a command-line tool over the same operations, that
  turns one declarative `Environment` manifest into running Machines and proves the
  result.

  The author writes one manifest with five sections — networks, machines, files,
  tasks and optional checks — and gets six operations over it:

  - **validate** — every error in the file at once, each with its field path, the
    rule broken and a fix hint, with no Docker involved.
  - **plan** — the fully resolved, side-effect-free description of what `apply`
    will do: images pinned by digest, Machine names and IPs, files and exact Task
    order. Deterministic, so the same manifest and image catalog always produce the
    same Plan.
  - **apply** — create the networks and Machines, write the declared files before
    the Machines start, then run the Tasks one at a time in dependency order. A
    Task succeeds only when its own result matches *and* all of its Probes pass at
    the same moment. Deliberate breakage is confirmed by the Probe on the Task that
    planted it, so a breakage that did not take effect fails the build.
  - **check** — run the Checks on request only, changing nothing, reporting each as
    pass, fail or error.
  - **status** — what exists, which Task each Environment last reached, the last
    run's result, and the files copied out to the host.
  - **destroy** — remove every Machine, network, volume and state file.

  The engine itself knows nothing about Kubernetes. A Machine stays a fresh Machine
  unless a Task bootstraps it; kubeadm, CNI installs, kubectl and Helm are ordinary
  commands that Tasks run. Everything below the Machine provider interface is
  backend-specific; everything above it is not, so a VM backend arrives later by
  implementing one interface and passing a conformance suite.

  ## User Stories

  ### Authoring and validating a manifest

  1. As a scenario author, I want unknown fields in my manifest to be errors, so that a typo in a field name fails loudly instead of silently doing nothing.
  2. As a scenario author, I want an unsupported `apiVersion` rejected with the supported versions named, so that I know what to change it to.
  3. As a scenario author, I want to see my manifest with all documented defaults applied, so that I can tell what the engine actually thinks I asked for.
  4. As a scenario author, I want every validation error reported at once rather than one per run, so that I can fix a whole file in one pass.
  5. As a scenario author, I want each validation error to carry its field path, the rule it broke and a hint at the fix, so that I can find and correct it without reading the engine's source.
  6. As a scenario author, I want validation to work with Docker stopped, so that I can write manifests on a machine that cannot run them.
  7. As a scenario author, I want a published JSON Schema for the manifest, so that my editor autocompletes fields and flags mistakes as I type.
  8. As a CI pipeline, I want to lint manifests against the published JSON Schema, so that a malformed manifest fails review before anyone runs it.
  9. As a scenario author, I want relative paths resolved against the manifest's own directory, so that an environment directory can be moved or copied without editing paths.
  10. As a scenario author, I want a `needs` entry that names no declared Task rejected, so that a renamed Task cannot leave a dangling dependency.
  11. As a scenario author, I want a cycle in the Task graph rejected, so that an unorderable manifest fails before anything is created.
  12. As a scenario author, I want a templated value that refers to a Task I do not depend on rejected, so that I cannot read output that may not exist yet.
  13. As a scenario author, I want duplicate Machine, Task or Check names rejected, so that one definition cannot silently shadow another.

  ### Seeing what will happen before it happens

  14. As a scenario author, I want planning to change nothing at all, so that I can inspect a plan on a live system safely.
  15. As a scenario author, I want the same manifest and image catalog to always produce the same Plan, so that a diff in the Plan means a real change in my manifest.
  16. As a scenario author, I want images pinned by digest in the Plan, so that a moving tag cannot change my environment underneath me.
  17. As a scenario author, I want Machine names and IPs shown in the Plan, so that I can write Probes and Checks against them before the first build.
  18. As a scenario author, I want the exact Task order in the Plan, so that I can confirm my `needs` produce the sequence I intended.
  19. As a scenario author, I want a manifest that needs a Capability the chosen backend lacks rejected before anything runs, with the field and the Capability named, so that I learn I need the VM backend in under a second instead of halfway through a build.
  20. As a scenario author, I want known host-affecting commands in Tasks, Probes and Checks rejected on the container backend, so that an environment cannot accidentally change my own machine's swap, clock or kernel modules.
  21. As a scenario author, I want the Plan to carry a hash identifying the build, so that the engine can tell an unchanged Environment from a changed one.

  ### Building the Environment

  22. As a scenario author, I want the networks I declare created, so that Machines on separate networks really are separated.
  23. As a scenario author, I want Machines created from an image or from a local build context, so that I can use a stock image or one I build myself.
  24. As a scenario author, I want to assign static IPs to Machines, so that a rebuilt Environment addresses them identically.
  25. As a scenario author, I want to set CPU and memory limits per Machine, so that a large Environment stays within my host's resources.
  26. As a scenario author, I want declared files written into a Machine before it starts, so that a service reads the configuration I intended on its very first start.
  27. As a scenario author, I want a Machine's name to be its hostname, and its Kubernetes node name once a Task bootstraps it, so that I can use exam-style names like `controlplane` and `node01`.
  28. As a scenario author, I want a Machine started from a Kubernetes node image to behave like a real node — systemd as init, kubelet as a service, containerd present — so that `kubeadm init`, `kubeadm join` and running kubeadm phase by phase all work.
  29. As a scenario author, I want the engine to contain no Kubernetes-specific behaviour, so that a Machine stays a fresh Machine until one of my Tasks bootstraps it and nothing happens behind my back.
  30. As a scenario author, I want images referenced through a catalog of aliases pinned by digest, so that I update a version in one place instead of in fifteen manifests.
  31. As a scenario author, I want the engine never to download manifests or charts itself, so that what my Environment installs is exactly the files I supplied.
  32. As a scenario author, I want locally built images built once on first use and reused afterwards, so that repeat builds do not rebuild what has not changed.
  33. As a scenario author, I want repeat builds to reuse cached images through a local registry cache, so that rebuilding is fast and works offline.
  34. As a scenario author, I want an `exec` Task to run a command on exactly one Machine, so that each step has one unambiguous place it happened.
  35. As a scenario author, I want a `transfer` Task to copy one file between a Machine, the host and the Environment's state directory, so that a kubeconfig can land where I can use it.
  36. As a scenario author, I want to set mode and owner on a transfer, so that a copied key has permissions its consumer accepts.
  37. As a scenario author, I want Tasks to run one at a time in a deterministic order that respects `needs`, so that two runs of the same manifest execute the same sequence.
  38. As a scenario author, I want a Task to succeed only when its own result matches and all of its Probes pass at the same moment, so that "succeeded" means the state I wanted actually held.
  39. As a scenario author, I want a Task retried up to its attempt limit, so that a flaky step does not fail an otherwise good build.
  40. As a scenario author, I want to choose whether a failed Task fails the build or is ignored, so that an optional step does not block the rest.
  41. As a scenario author, I want a Task to be able to require a *failing* command, so that I can assert a command is broken as part of planting a problem.
  42. As a scenario author, I want a Probe re-run at its interval until it matches, so that I can wait for a condition instead of guessing a sleep.
  43. As a scenario author, I want a Task with no command and only Probes, so that I can wait for a state such as a node becoming NotReady.
  44. As a scenario author, I want a Task to start a long-running background process, so that an Environment can include a running load generator or a port-forward.
  45. As a scenario author, I want a background process stopped before its Task re-runs and removed with its Machine, so that retries and destroys do not leave orphans.
  46. As a scenario author, I want commands, paths, environment variables and Probes to use earlier Tasks' output and exit codes, so that a join command produced on the control plane can run on a worker without my copying it by hand.
  47. As a scenario author, I want to reference Machine IPs in commands and Probes, so that I can address a Machine whose IP the engine allocated.
  48. As a scenario author, I want my Environment counted as provisioned when every Task has succeeded, so that a deliberately broken end state is a valid, finished result rather than a failure.
  49. As a scenario author, I want a planted breakage proven by the Probe on the Task that planted it, so that a breakage which did not take effect fails the build instead of shipping silently.

  ### Verifying the result

  50. As a scenario author, I want Checks to run only when I ask, so that `apply` never runs them and a build is not confused with a verification.
  51. As a scenario author, I want running Checks to change nothing, so that I can verify a live Environment as often as I like.
  52. As a scenario author, I want a Check to accept the same fields as an `exec` Task apart from dependencies and background mode, so that I do not learn a second syntax.
  53. As a scenario author, I want each Check to run independently of the others, so that one failure does not hide the rest.
  54. As a scenario author, I want each Check result to record pass, fail or error, its duration and the output observed, so that a failure tells me what the Environment actually looked like.
  55. As a scenario author, I want the Check run to pass only when every Check passes, so that one exit code answers "is this Environment correct".
  56. As a scenario author, I want a Check that could not run reported as error rather than fail, so that I can tell a broken Check from a broken Environment.

  ### Re-applying, interrupting and cleaning up

  57. As a scenario author, I want `apply` on a host with no such Environment to create one, so that the first run needs no extra flags.
  58. As a scenario author, I want `apply` on a complete Environment of the same Plan to report up to date and change nothing, so that re-running is safe and quick.
  59. As a scenario author, I want `apply` on a changed Plan refused unless I ask to recreate, so that I never silently lose an Environment I was working in.
  60. As a scenario author, I want editing only Checks never to force a rebuild, so that I can refine verification against an Environment that is already built.
  61. As a scenario author, I want an Environment whose last apply failed or was interrupted marked incomplete and never reported up to date, so that I am not told a half-built Environment is ready.
  62. As a scenario author, I want the next apply after a failure to rebuild from scratch, so that steps like `kubeadm init` never re-run on a partly built Machine.
  63. As a scenario author, I want `destroy` to remove every Machine, network, volume and state file, so that nothing is left to pay for or trip over.
  64. As a scenario author, I want `destroy` to succeed when nothing exists, so that cleanup in a script never needs a guard.
  65. As a scenario author, I want to list Environments with their Machines, the last Task reached and the last run's result, so that I can see what is on my host.
  66. As a scenario author, I want to run an ad-hoc command on a Machine, so that I can look around without writing a Task.
  67. As a scenario author, I want to read the stored log of any run, so that I can diagnose a failure after the fact.
  68. As a scenario author, I want to list the files copied out to the host, so that I can find the kubeconfig a Task delivered.
  69. As an operator, I want Ctrl-C to stop work within ten seconds, so that I can abandon a slow build without killing it by hand.
  70. As an operator, I want a killed engine process never to block the next apply or destroy, so that a crash is recoverable without manual cleanup.
  71. As a scenario author, I want an unchanged Environment to build with no internet access after the first run, so that I can work offline.

  ### Using the engine from software

  72. As the CKA Exam Simulator, I want the library and `simctl` to offer the same six operations, so that what I automate matches what an author does by hand.
  73. As the CKA Exam Simulator, I want typed progress events streamed while a build runs — Task started and finished, command output, Probe and Check results, warnings and errors — so that I can show progress rather than a spinner.
  74. As a scenario author at the terminal, I want the same events as JSON lines, so that I can pipe a build into my own tooling.
  75. As the CKA Exam Simulator, I want every apply and Check run to write a versioned JSON report, so that I can parse results without scraping text and without breaking on the next release.
  76. As an operator, I want every command and its output kept in a full run log, so that nothing about a run is lost once the process exits.
  77. As a CI pipeline, I want exit codes that distinguish an invalid manifest, a missing Capability, a provisioning error, a failed Task, a failed Check, a Check that could not run, and an interruption, so that a job can react to the kind of failure.
  78. As the CKA Exam Simulator, I want report formats to change only additively within an API version, so that an engine upgrade does not break my parsing.

  ### Extending and testing the engine

  79. As an engine developer, I want one Machine provider interface to be the only place a backend is implemented, so that adding a VM backend touches nothing above it.
  80. As an engine developer, I want a published conformance suite any provider can be run against, so that "this backend works" is a test result rather than an opinion.
  81. As an engine developer, I want an in-memory fake provider shipped in the repository, so that I can test the whole engine with Docker stopped.
  82. As an engine developer, I want time injected rather than read from the system clock, so that Probe intervals, timeouts and retries can be tested in milliseconds instead of minutes.
  83. As an engine developer, I want the spec and planner to be pure functions, so that I can test them exhaustively against a corpus of manifests with no setup.
  84. As an engine developer, I want the unit tests above the Machine provider to finish in under sixty seconds, so that I run them on every change.
  85. As an engine developer, I want every backend call and command bounded by a timeout, so that no operation can hang forever.
  86. As an engine developer, I want transient backend errors retried with backoff, so that a momentary Docker hiccup does not fail a long build.
  87. As an engine developer, I want errors to name the file, field path or Task, say what was expected and suggest a fix, so that users can self-serve.
  88. As an engine developer, I want structured logs with a debug level that shows full backend and command output, so that I can diagnose a failure I cannot reproduce.
  89. As an operator, I want keys and secrets kept in the state directory with owner-only permissions, so that other users on the host cannot read them.
  90. As an operator, I want Machines of different Environments not to share networks unless declared, so that two Environments cannot interfere.

  ## Implementation Decisions

  ### Module boundaries

  Ten modules. The dependency direction is strictly downward; nothing above
  `provider` references Docker.

  | Module | Responsibility |
  | --- | --- |
  | `spec` | Decode manifest bytes strictly, apply documented defaults, validate structure and references. Produces a defaulted `Environment` or a sorted list of field-path errors. Pure. |
  | `catalog` | Resolve image aliases and OCI references to digests. The only module that talks to a registry. |
  | `planner` | `Environment` + resolved image catalog → `Plan`: pinned digests, Machine names, allocated IPs, files, Task order, Plan hash. Gates Capabilities and the host-affecting-command denylist. Pure. |
  | `provider` | The Machine provider interface, plus the `container` and `fake` implementations and the exported conformance suite. The single Docker boundary. |
  | `executor` | Drives a `Plan`: create networks, create Machines, write files, start Machines, wait for reachability, then walk the Task graph. |
  | `taskrunner` | Runs one Task to a verdict: criterion 1, Probes, attempts, timeout, background lifecycle, template resolution. |
  | `checkengine` | Runs Checks one at a time in declaration order, each independent, each ending pass, fail or error. |
  | `state` | The Environment state directory: Plan, incomplete marker, reports, run logs, transferred files. Owns permissions. |
  | `events` | The typed event taxonomy and its JSON-lines rendering. |
  | `cli` | `simctl`. A thin adapter over the library: parse argv, render events and reports, map outcomes to exit codes. |

  ### The Machine provider interface

  This is the central architectural decision, because NFR-9 requires a new backend
  to be added by implementing one interface and passing the conformance suite,
  without editing the executor. The interface is deliberately small and offers only:

  - declare which Capabilities it supplies
  - create and delete a network
  - create a Machine (image or build context, networks, IPs, limits, files staged
    before start), start it, and report when it is reachable
  - delete a Machine with its volumes
  - run a command on a Machine, with a user, environment, timeout and captured
    output and exit code
  - start and stop a named background process on a Machine
  - copy a file into and out of a Machine
  - enumerate the Machines, networks and volumes belonging to an Environment, so
    that state can be rebuilt from backend metadata after a crash

  Nothing Kubernetes-shaped appears in it. Nothing Docker-shaped leaks through it:
  no container IDs, no image-pull semantics, no Docker error types.

  ### Decisions taken

  - **Six operations are the library surface.** `Validate`, `Plan`, `Apply`,
    `Check`, `Status`, `Destroy`. `simctl` adds no behaviour of its own; a feature
    reachable from the CLI but not the library is a bug.
  - **Every waiting operation takes an injected clock.** Probe intervals, Task
    timeouts and retry windows are measured against it, never against the system
    clock. This changes the signature of the executor, the task runner and the
    probe loop, and is the reason NFR-10's sixty-second unit-test budget is
    achievable at all.
  - **Digest resolution is separated from planning.** `catalog` resolves aliases
    and tags to digests; `planner` receives the resolved catalog as an input. This
    is what makes planning both pure and deterministic (FR-7) while still pinning
    by digest, and what lets planner tests run with no network.
  - **The Plan hash identifies a build, and excludes Checks.** `apply` compares the
    manifest's Plan hash against the stored one to decide create, up-to-date or
    refuse. Because Checks are not part of the Plan, editing them never forces a
    rebuild (FR-25).
  - **An incomplete marker is written before the first mutation and cleared only on
    full success.** Its presence means rebuild from scratch, never report up to
    date (FR-26). It is a file in the state directory, so a killed process leaves
    it behind by construction rather than by cleanup.
  - **State is reconstructible from backend metadata plus the state directory.**
    Every created resource is tagged with its Environment name, so `status` and
    `destroy` work even if the state directory is lost, and a crash never blocks
    the next run (NFR-7).
  - **Templates resolve immediately before a Task runs,** from Tasks named in its
    `needs` and from declared Machines only. Validation rejects any other
    reference, so resolution cannot fail at run time for a reference reason.
  - **Background processes are named per Task and owned by the Machine.** Each is a
    transient unit named after its Task, which gives the task runner a stable
    handle to stop before a retry, and makes removal with the Machine automatic.
    A background Task cannot use criterion 1, so its Probes decide.
  - **Transfer endpoints are one of three shapes:** a Machine path, a local file
    relative to the manifest directory, or a path in the Environment's state
    directory. Owner is meaningless for the last and is rejected rather than
    ignored.
  - **Execution is sequential, full stop.** The Task order is the first Task in
    declaration order whose `needs` are satisfied, which makes the order a property
    of the Plan rather than of the run. Parallelism is out of scope and no
    concurrency is designed in speculatively.
  - **The Capability model is provider-declared and planner-enforced.** Providers
    publish the Capabilities they supply; the planner rejects a manifest needing
    one that is absent, naming both field and Capability. `backend: vm` is a
    recognised, reserved value that is rejected with a clear message rather than an
    unknown-value error.
  - **The host-affecting-command guard is a plan-time denylist** over Tasks, Probes
    and Checks. It is documented as an accident guard, not isolation; real
    isolation arrives with the VM backend.
  - **Reports are versioned and additive within a version.** Both the apply report
    and the Check report carry a schema version. Within a manifest API version,
    fields may only be added (NFR-12).
  - **One event taxonomy serves both consumers.** The library streams typed events;
    the CLI renders those same events as JSON lines. There is no second, CLI-only
    event path.
  - **Exit codes distinguish seven outcomes** — invalid manifest, missing
    Capability, provisioning error, failed Task, failed Check, Check that could not
    run, interruption — and are assigned in one place in `cli`.
  - **The JSON Schema is generated from the spec types,** not maintained by hand,
    so it cannot drift from what the decoder accepts.
  - **Secrets in the state directory are owner-only,** set at creation rather than
    fixed afterwards.

  ## Testing Decisions

  ### What makes a good test here

  A good test drives the engine the way a caller does and asserts on what a caller
  can observe: the returned report, the streamed events, and the state the next
  operation reports. It survives a refactor of how the work is divided internally.

  Specifically, for this codebase:

  - **Assert on reports and events, never on provider call logs.** The fake
    provider records what it was asked to do, and that record is for debugging, not
    for assertions. A test that asserts "the provider was asked to create three
    Machines" is coupled to the executor's internals; the equivalent behavioural
    assertion is that `status` reports three Machines, or that the apply report
    lists them.
  - **Verify through the interface, not around it.** To test that a transfer
    delivered a kubeconfig, read it back through the operation that lists
    host-delivered files — not by stat-ing a path the implementation chose.
  - **One logical assertion per test,** and a name that says what holds, not how it
    is achieved.
  - **No mocking of internal collaborators.** The only substituted dependencies are
    the ones the architecture already injects: the Machine provider, the clock and
    the image catalog.

  ### The seams, and what is tested at each

  | Seam | What is tested there |
  | --- | --- |
  | **S1 — the library API** | The primary seam and the large majority of tests. Whole manifests driven through validate → plan → apply → check → status → destroy against the `fake` provider, with an injected clock. Covers Task success criteria, Probes, wait Tasks, retries, `onFailure`, background lifecycle, templates, transfers, Check verdicts, the create-only apply state machine, the incomplete marker, and crash recovery. |
  | **S2 — the Machine provider interface** | An exported conformance suite, parameterized over any provider, asserting the contract: files staged before start, reachability, command execution and output capture, background start/stop, file copy both ways, resource enumeration, and complete deletion. Run against `fake` on every change, and against `container` behind a build tag that requires Docker. This suite is the deliverable that makes NFR-9 checkable. |
  | **S3 — `spec` and `planner` as pure functions** | Table-driven tests, plus the negative corpus below. `spec`: strict decoding, defaulting, and every validation rule, each asserting the exact field path. `planner`: golden-file `Plan` comparison for determinism, IP allocation, Task ordering, Plan hashing, Capability gating and the command denylist. No Docker, no network, no clock. |
  | **S4 — the `simctl` CLI** | Only CLI-specific behaviour: argv parsing, the exit-code mapping for all seven outcomes, JSON-lines rendering, and human-readable output. Invoked in-process with captured streams. Engine behaviour is not re-tested here. |
  | **S5 — end-to-end against real Docker** | Build-tagged, deliberately few. Proves what the fake cannot: that the `container` provider really produces reachable Machines with working systemd, that `destroy` leaves zero containers, networks and volumes, that an interrupt stops within ten seconds, and that destroy-then-apply reproduces the same Plan, names and IPs. |

  ### Modules tested, and where

  | Module | Primary seam | Notes |
  | --- | --- | --- |
  | `spec` | S3 | 80% line coverage target; the negative corpus lives here |
  | `planner` | S3 | 80% line coverage target; golden Plans |
  | `catalog` | S3 | Resolution tested against a recorded catalog; no live registry in unit tests |
  | `provider` (`fake`) | S2 | Must pass the same conformance suite as `container` |
  | `provider` (`container`) | S2, S5 | Build-tagged; Docker required |
  | `executor` | S1 | Through the library API, never directly |
  | `taskrunner` | S1 | Through the library API; the clock makes timing assertions exact |
  | `checkengine` | S1 | 80% line coverage target |
  | `state` | S1 | Verified through `status` and through recovery behaviour |
  | `events` | S1, S4 | Typed stream at S1, JSON-lines rendering at S4 |
  | `cli` | S4 | Exit codes and rendering only |

  ### Prior art

  There is none in this repository — it contains no code yet, only `prd.md` and
  `manifest-layout-design.md`. These tests therefore establish the conventions
  rather than follow them, which is itself a reason to get the seams right in the
  first slices. The conventions to establish:

  - Go table-driven tests as the default shape.
  - Golden files for Plans and reports, with a regeneration flag.
  - Build tags for anything requiring Docker, so the default test invocation never
    needs it.
  - The conformance suite as an exported package, not an internal one, so a backend
    developed outside this repository can run it.
  - An in-memory fake as a first-class shipped artefact, maintained with the
    interface rather than retrofitted.

  Outside this repository, kind and kinder are the reference for container settings
  and for kubeadm phase handling, but they are prior art for the implementation,
  not for the tests.

  ### Specific test corpora required

  - **A negative corpus of at least thirty invalid manifests**, each paired with the
    field path its error must name. This is an acceptance criterion in `prd.md` and
    the main defence against the risk that the manifest grows faster than its
    validation.
  - **Golden Plans** for a representative manifest set, asserting determinism across
    runs and making any change in planning visible in review.
  - **A reproducibility test**: destroy then apply yields the same Plan, Machine
    names and IPs.

  ## Out of Scope

  Out of scope for this PRD, but in scope for engine v1 in a **separate content
  PRD**:

  - FR-32, the reference kubeadm configuration for kind node images.
  - FR-33, the catalog of fifteen example Environments run end to end in CI.
  - FR-34, the apt-managed Kubernetes node image for package-based upgrades.
  - The NFR-1 benchmark figure, which depends on a specific example Environment and
    so can only be measured once that example exists.

  Out of scope and worth stating explicitly:

  - **The kubeadm-in-containers spike.** `prd.md` section 11 names this the largest
    risk and says to prove it first. It is not a prerequisite for this PRD: engine
    core is built and tested against the `fake` provider and plain Linux images,
    and FR-13 guarantees the engine holds no Kubernetes behaviour to be affected by
    the outcome. It *is* a prerequisite for the content PRD, and belongs in its own
    spike. If the spike forces a fallback to the kind library, that is a scope
    change to `prd.md`, not to this PRD.
  - The VM backend and snapshots. `backend: vm` is reserved and rejected here.
  - Reuse of shared Task blocks, such as a standard kubeadm bootstrap. Accepted for
    v1 that examples are copied and adapted.
  - Resuming an incomplete Environment from its first unfinished Task. FR-26
    rebuilds from scratch instead.
  - Parallel Task execution.
  - Reverting faults, Check weights, and Checks that restart components.
  - Everything belonging to the CKA Exam Simulator: web UI, terminal, exam
    sessions, timers, scoring weights and presentation.
  - Multiple users, remote operation, authentication.
  - Any host other than Linux with Docker Engine. macOS, Windows, Docker Desktop,
    Podman, OrbStack and Colima are explicitly unsupported.

  ## Further Notes

  **An open question touches FR-4.** `prd.md` records that the product name and the
  final API group are undecided, with `sim.dev` as a placeholder, to be settled
  before the JSON Schema is published. Generating the schema from the spec types
  means the group appears in a generated artefact; the slice that publishes the
  schema is the one that needs the answer. Earlier slices can proceed on the
  placeholder.

  **`CONTEXT.md` does not exist yet.** Until it does, `prd.md` section 5 is the
  authoritative vocabulary, as recorded in `docs/agents/domain.md`. The term that
  matters most for this PRD is the one defined by exclusion: a *Cluster* is not an
  engine concept. A Kubernetes cluster exists only because Tasks ran kubeadm, and
  the engine never names, reads or validates clusters. Issue titles, module names
  and test names in this work should not acquire cluster-level language.

  **This repository is not under version control.** There is no `.git` directory, so
  `prd.md`, `manifest-layout-design.md` and this PRD are currently unversioned.
  Worth fixing before implementation begins.

  **On sequencing.** The natural first slice is a walking skeleton: one Machine, one
  `exec` Task, one Check, driven through all six operations against both the `fake`
  and `container` providers. It establishes every one of the five seams at their
  thinnest and makes each later slice a thickening of a layer that already has a
  test harness around it. The order of thickening that follows the dependencies
  rather than the FR numbering is roughly: Task success criteria and Probes, then
  retries and background, then transfers and templates, then multiple Machines,
  networks and IPs, then the apply state machine and crash recovery, then reports,
  events and exit codes.
