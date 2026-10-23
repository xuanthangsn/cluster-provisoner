# Cluster Provisioning Engine — PRD (v1, Container Backend)

Oct 13, 2026 · @txthang

## Document information

This PRD defines release v1 of a standalone cluster provisioning engine. The CKA Exam Simulator will be built on it later as a separate product.

| Field | Value |
| --- | --- |
| Product | Cluster Provisioning Engine (working name): a Go library plus the `simctl` command-line tool |
| Release | v1, container backend |
| Status | Draft for review |
| Owner | @txthang |
| First planned consumer | CKA Exam Simulator (separate product, separate PRD) |
| Design documents | [Manifest Layout Design] |
This PRD owns product intent: problem, users, scope, required behaviour and acceptance.&#32;

**Change log**

| Date | Change |
| --- | --- |
| 2026-10-13 | Rewritten as an engine-only PRD. Added problem statement, roadmap, outcome metrics and scope coverage. Moved interface and implementation detail to the design documents. Fixed contradictions in apply semantics, status output and stale field names. |
| 2026-10-08 | First draft, written as Phase 1 of the CKA Simulator. |

## 1. Summary

The engine turns one declarative YAML file into a running set of Linux machines on the user's own computer, configured by ordered commands, and proves the result with optional checks. Kubernetes clusters, including deliberately broken ones, are its main use. The engine itself knows nothing about Kubernetes: kubeadm, CNI installs and kubectl are ordinary commands that the file's tasks run.

- **Input:** one `Environment` manifest with five sections: networks, machines, files, tasks and optional checks.
- **Output:** running machines, files copied out to the host (such as a kubeconfig), and machine-readable reports of provisioning and checks.
- **Form:** a Go library, plus `simctl`, a command-line tool over the same operations for authors and CI.
- **v1 backend:** machines are Docker containers on a Linux host, usually from the `kindest/node` image.
- **Later:** a VM backend for scenarios containers cannot express, and the CKA Exam Simulator as the first product built on the engine.

## 2. Problem

There is no local tool that builds a Kubernetes environment in a precise, possibly broken state, the same way every time, and proves it reached that state. Practice and test environments, especially for troubleshooting, need exactly that.

| Today's option | What it does well | Gap |
| --- | --- | --- |
| kind config plus a shell script | Fast local clusters | Creates the cluster, but everything after that is configured by hand or ad-hoc scripts, so the end state isn't defined anywhere and can't be reproduced or verified. |
| Hand-built VMs (for example with Vagrant) | Real kernels and full fidelity | Provisions machines only; all cluster setup and breakage is manual afterwards. Slow to build and reset. |
| Hosted practice platforms | Ready-made scenarios | Scenarios belong to the platform. You cannot author your own, run them offline, or build a product on them. |

**Value proposition.** The engine provisions machines and configures them automatically, exactly as a predefined file describes, with no manual steps afterwards. That file is reviewed and versioned, and applying it builds the same environment every time. Probes confirm that every step, including each deliberate breakage, actually took effect. Checks in the same file verify the end state, and the same checks can later grade a learner's fix.

**Why now.** The CKA Exam Simulator needs exactly this as its foundation. Building the engine first, as a product with its own users and acceptance criteria, lets it be proven before any exam UI depends on it.

## 3. Vision and roadmap

The engine is a general provisioning layer; the CKA Exam Simulator is the first product built on top of it. Everything specific to the exam (sessions, timers, scoring weights, the web terminal) stays in the simulator. The engine supplies environments and verdicts.

| Release | Contents | Status |
| --- | --- | --- |
| Engine v1 (this PRD) | Container backend, sequential tasks with probes, checks, `simctl` CLI and Go API | Draft |
| Engine v2 | VM backend for scenarios that need their own kernel, clock or real resource limits; a reuse mechanism for shared tasks | Not started |
| CKA Exam Simulator | Web UI, terminal, exam sessions and timers, grading presentation; consumes the engine | Separate PRD |

**What the simulator will need from the engine, and v1 must therefore get right:**

| Need | Covered by |
| --- | --- |
| Build a session's environment from a manifest file and show progress while it builds | FR-10 to FR-21, FR-30 |
| Grade a candidate's answer with the environment's checks, without changing anything | FR-22 to FR-24 |
| Remove a session's environment completely afterwards | FR-27 |
| Embed the engine as a library with stable, versioned report formats | FR-29 to FR-31 |
| Move scenarios to the VM backend later without rewriting them | NFR-9 |
| Nodes that look like exam nodes: real kubeadm clusters, systemd, kubelet as a service, exam-style names | FR-11, FR-12, FR-32 |

## 4. Users and use cases

The primary user of v1 is the scenario author. The engine developer is secondary, and two pieces of software consume the engine: the future CKA Exam Simulator and CI pipelines.

| User | Who | Job to be done |
| --- | --- | --- |
| Scenario author (primary) | Writes practice, test or demo environments; CKA troubleshooting scenarios first | "Describe an environment once, healthy or broken, rebuild it exactly every time, and prove it is in the intended state." |
| Engine developer | Builds and extends the engine | "Add a backend, task type or check feature without touching unrelated code, and test it without Docker." |
| CKA Exam Simulator (software, later) | First product built on the engine | "Build a session's environment, follow its progress, grade answers with its checks, and remove it afterwards." |
| CI pipeline (software) | Automated runs of example catalogs | "Prove every environment still builds and passes its checks on each change." |

**User stories**

| ID | Story | Accepted when |
| --- | --- | --- |
| US-1 | As a scenario author, I describe machines, files and ordered commands in one file and build it with one command. | `simctl apply` on a new environment ends with every task succeeded and exit code 0, with no manual steps. |
| US-2 | As a scenario author, I plant deliberate problems and know they took effect during the build. | A breakage task with a probe fails the build if the breakage did not happen. |
| US-3 | As a scenario author, I verify the final state at any time without changing it. | `simctl check` reports each check as pass, fail or error and leaves the environment untouched. |
| US-4 | As a scenario author, I find every mistake in my file before anything runs. | `simctl validate` lists all errors at once, each with a field path and a fix hint, without Docker. |
| US-5 | As a scenario author, I rebuild the same environment after destroying it. | Destroy then apply produces the same plan, machine names and IPs. |
| US-6 | As the CKA Exam Simulator, I embed the engine and follow a build as it happens. | The Go API streams typed progress events and returns the same reports as the CLI. |
| US-7 | As an engine developer, I add a machine backend. | Implementing one interface and passing the conformance suite is enough; nothing above the machine layer changes. |

## 5. Key concepts

Every later section uses these terms with exactly these meanings.

| Term | Meaning |
| --- | --- |
| Environment | The single manifest, and what it builds: networks, machines, files, tasks and optional checks. |
| Machine | A Linux system declared in the manifest. In v1 it is a Docker container; with the VM backend it will be a VM. It becomes a Kubernetes node only if a task bootstraps it. |
| Machine provider | The backend that creates, runs commands on, copies files to and from, and deletes machines. v1 has one: `container`. |
| Task | One step, of one of two types: `exec` runs a command on one machine; `transfer` copies one file. Tasks run one at a time in dependency order. |
| Probe | A read-only command repeated until its result matches. A task succeeds only when its own result and all its probes pass at the same time. |
| Check | An optional, independent verification command, run only on request. Checks never change the environment and can later serve as a grader. |
| Capability | Something a provider can supply, such as `kernel-modules`, `swap` or `clock`. A manifest that needs a capability the backend lacks is rejected before anything runs. |
| Plan | The fully resolved, side-effect-free description of what `apply` will do: pinned images, machine names and IPs, files and task order. Its hash identifies an environment's build. |
| Cluster | Not an engine concept. A Kubernetes cluster exists only because tasks ran kubeadm; the engine never names, reads or validates clusters. |

## 6. Scope

v1 builds and verifies environments on one Linux host with Docker Engine, for one local user. Everything about exams, and everything that needs a real kernel per machine, is out.

**In scope for v1**

- The `Environment` manifest, its validation and a published JSON Schema.
- A pure planner and the create-only apply, check, status and destroy lifecycle.
- The container machine provider, a backend-neutral provider interface, and a conformance suite for providers.
- Exec and transfer tasks with probes, retries, background tasks and templated values.
- Checks with machine-readable reports.
- The `simctl` CLI and the Go library.
- Shipped content: a reference kubeadm configuration for kind node images, an apt-managed node image for package-based upgrades, and a catalog of 15 example environments, all kept in the engine repository for v1.

**Out of scope**

| Item | Where it belongs |
| --- | --- |
| Web UI, terminal, exam sessions, timers, scoring weights and presentation | CKA Exam Simulator |
| VM backend and snapshots (`backend: vm` is reserved and rejected) | Engine v2 |
| Reuse of shared task blocks, such as a standard kubeadm bootstrap | Engine v2 |
| Resuming an incomplete environment from its first unfinished task (v1 rebuilds it from scratch, FR-26) | Later release |
| Parallel task execution | Later release |
| Reverting faults, check weights, checks that restart components | Later release or the simulator |
| Multiple users, remote operation, authentication | Not planned |

**Platform constraints.** v1 runs only on Linux (amd64, or arm64 with arm64 node images) with Docker Engine. macOS, Windows, Docker Desktop, Podman, OrbStack and Colima are not supported.

**Scenario coverage on the container backend**

This table is the practical scope statement for scenario authors.

| Scenario type | v1 |
| --- | --- |
| Node never joined; fresh machine left for the learner to join | Yes |
| kubelet misconfigured, stopped or using a wrong certificate | Yes |
| Broken control-plane static pod manifest (API server, scheduler, controller manager, etcd) | Yes |
| No CNI, broken CNI config, kube-proxy skipped | Yes |
| kubeadm run phase by phase, with steps between phases | Yes |
| Workload, Service, RBAC and storage problems inside the cluster | Yes |
| Multiple clusters with a jump host holding several contexts | Yes |
| Package-based `kubeadm upgrade` | Yes, on the apt-managed node image (FR-34) |
| Swap, kernel modules, kernel sysctls, clock skew | No: needs the VM backend |
| Memory, PID or disk pressure on a node | No: needs the VM backend |

## 7. Functional requirements

Requirements state observable behaviour; how each is built lives in the design documents. MUST means required for v1; SHOULD means desirable and may slip.

**Describe: writing and validating a manifest**

- **FR-1** The engine MUST decode manifests strictly: unknown fields are errors. It MUST reject an unsupported `apiVersion` and name the supported ones.
- **FR-2** The engine MUST apply documented defaults before validation and let the author see the fully defaulted manifest.
- **FR-3** Validation MUST report every error at once, each with its field path (for example `tasks.join-node02.needs[0]`), the rule broken and a fix hint. It MUST NOT need Docker.
- **FR-4** The engine MUST publish a JSON Schema for the manifest, for editor autocomplete and CI linting.
- **FR-5** Relative paths in a manifest MUST resolve against the manifest's own directory.
- **FR-6** Every task dependency MUST name a declared task and the dependencies MUST NOT form a cycle. A templated value MAY refer only to a task the referring task depends on.

**Plan: seeing what will happen before it happens**

- **FR-7** Planning MUST have no side effects and MUST be deterministic: the same manifest and image catalog always give the same plan, with images pinned by digest, machine names and IPs, files and the exact task order.
- **FR-8** The planner MUST reject a manifest that needs a capability the selected backend lacks, naming the field and the capability.
- **FR-9** On the container backend, the planner MUST reject known host-affecting commands (such as `swapon`, `date -s`, `modprobe`, `sysctl -w kernel.*`) in tasks, probes and checks. This is a guard against accidents, not isolation.

**Provision: building the environment**

- **FR-10** The engine MUST create the declared networks and machines, from an image or a local build, with optional static IPs and CPU and memory limits, and write declared files into each machine before it starts.
- **FR-11** A machine's name MUST be its hostname and, once a task bootstraps it, its Kubernetes node name, so environments can use exam-style names such as `controlplane` and `node01`.
- **FR-12** A machine started from a Kubernetes node image MUST behave like a real node: systemd as the init process, kubelet as a service, containerd, and support for `kubeadm init`, `kubeadm join` and running kubeadm phase by phase.
- **FR-13** The engine MUST contain no Kubernetes-specific behaviour. A machine stays a fresh machine unless a task bootstraps it.
- **FR-14** Images MUST be referenced through a catalog of aliases pinned by digest. The engine MUST NOT download manifests or charts itself; scenarios supply them as files. Example images such as the tools image MUST be built locally on first use and reused afterwards. Repeat builds SHOULD reuse cached images through a local registry cache.
- **FR-15** An `exec` task MUST run a command on one machine. A `transfer` task MUST copy one file from a machine or the host to a machine or to the environment's state directory on the host (for example a kubeconfig), with optional mode and owner.
- **FR-16** Tasks MUST run one at a time in a deterministic order that respects dependencies.
- **FR-17** A task MUST succeed only when its own result matches (exit code and optional output conditions) and all its probes pass at the same time, within its timeout. It MUST retry up to its attempt limit and then either fail the build or be ignored, as the manifest says.
- **FR-18** A probe MUST run repeatedly at its interval until it matches. A task with no command and only probes MUST be allowed, so a manifest can wait for a state such as a node becoming NotReady.
- **FR-19** A task MUST be able to start a long-running background process that is stopped before the task re-runs and removed with its machine.
- **FR-20** Commands, paths and probes MUST be able to use the output and exit code of earlier tasks and the IPs of machines.
- **FR-21** An environment MUST count as provisioned when every task has succeeded. A deliberately broken end state is valid; it is confirmed by tasks with probes, not by a separate readiness section.

**Verify: checking the result**

- **FR-22** Checks MUST run only when the user asks for them. `apply` MUST NOT run checks, and running checks MUST NOT change the environment.
- **FR-23** A check MUST accept the same fields as an `exec` task, except dependencies and background mode, pass by the same criteria, and run independently of other checks.
- **FR-24** Each check result MUST record status (pass, fail or error), duration and observed output. The run passes only when every check passes.

**Lifecycle: re-applying, interrupting and cleaning up**

- **FR-25** `apply` MUST be create-only. With no such environment, it creates one. With a complete environment of the same plan, it reports up to date and changes nothing. With a different plan, it MUST refuse unless the user asks to recreate. Checks are not part of the plan, so editing them never forces a rebuild.
- **FR-26** An environment whose last apply failed or was interrupted MUST be marked incomplete, and MUST NOT be reported as up to date. The next apply MUST rebuild it from scratch, because re-running steps such as `kubeadm init` on a partly built machine is not safe.
- **FR-27** `destroy` MUST remove every machine, network, volume and state file of the environment, and MUST succeed when nothing exists.
- **FR-28** The user MUST be able to list environments with their machines, the last task reached and the last run's result; run a command on a machine; read the stored log of any run; and list files copied to the host.

**Integrate: using the engine from software**

- **FR-29** The Go library and `simctl` MUST offer the same operations: validate, plan, apply, check, status and destroy.
- **FR-30** The engine MUST stream typed progress events (task started and finished, command output, probe and check results, warnings and errors) to library callers and, as JSON lines, to the CLI.
- **FR-31** Every apply and check MUST write a versioned JSON report and a full run log. Exit codes MUST distinguish an invalid manifest, a missing capability, a provisioning error, a failed task, a failed check, a check that could not run, and an interruption. Schemas and code values live in Engine Internals.

**Shipped content**

- **FR-32** v1 MUST ship a reference kubeadm configuration that works on kind node images.
- **FR-33** v1 MUST ship the 15 example environments listed in section 9, each run end to end in CI.
- **FR-34** v1 MUST ship an apt-managed Kubernetes node image that keeps the guarantees of FR-12 and installs kubeadm, kubelet and kubectl from the package manager, so tasks and learners can run a package-based `kubeadm upgrade` to the next minor version.

## 8. Non-functional requirements

The engine must be fast enough for repeated authoring, safe to interrupt, and testable without Docker above the machine layer.

| ID | Area | Requirement |
| --- | --- | --- |
| NFR-1 | Performance | Building the benchmark environment (1 control plane, 2 workers, CNI; `examples/bench-1cp-2w`) from a warm image cache: goal under 90 s on 4 cores and 8 GB RAM. Confirmed or re-baselined before v1 acceptance. |
| NFR-2 | Performance | `validate` and `plan` finish in under 1 s for a 3-cluster environment, with no network access. |
| NFR-3 | Performance | Applying an unchanged, complete environment makes no changes and finishes in under 10 s. |
| NFR-4 | Footprint | Engine idle memory under 100 MB; each environment costs only its machines plus the shared image cache. |
| NFR-5 | Reliability | Every backend call and command has a timeout; transient Docker errors are retried with backoff, up to 3 times. |
| NFR-6 | Interruption | Ctrl-C or context cancellation stops work within 10 s. After `destroy`, zero containers, networks or volumes remain. |
| NFR-7 | Crash safety | What exists is rebuilt from backend metadata and the state directory; a killed process never blocks the next apply or destroy. |
| NFR-8 | Offline | After the first run, an unchanged environment builds with no internet access. |
| NFR-9 | Backend neutrality | Nothing above the machine provider depends on Docker. A new backend is added by implementing one interface and passing the conformance suite, without editing the executor. |
| NFR-10 | Testability | Spec, planner, executor, task runner and check engine run against an in-memory fake provider; their unit tests finish in under 60 s, with at least 80% line coverage for spec, planner and checks. |
| NFR-11 | Security | Tasks, probes and checks never run on the host. Keys and secrets stay in the state directory with 0600 permissions. Machines of different environments do not share networks unless declared. |
| NFR-12 | Compatibility | Within a manifest API version, fields may only be added. Changing a field's meaning or removing it needs a new version and a conversion. |
| NFR-13 | Usability | Every error names the file, field path or task, says what was expected, and suggests a fix. |
| NFR-14 | Observability | Structured logs; a debug level shows full backend and command output; every command and its output is kept in the run log. |

## 9. Success metrics and acceptance

v1 succeeds when an author can describe any environment in the coverage table once and get it built, verified and rebuilt identically on demand.

**Outcome metrics**

| Metric | Target | How measured |
| --- | --- | --- |
| Time to author a new broken-cluster scenario by adapting an example | Under 30 minutes | Timed by the author for 3 new scenarios before v1 acceptance |
| Coverage of v1 scenario types | Every "Yes" row in the coverage table (section 6) has at least one example | Example catalog review |
| Reproducibility | Destroy then apply gives the same plan, names and IPs, and all checks still pass | CI end-to-end job |

**Quality metrics**

| Metric | Target | How measured |
| --- | --- | --- |
| Examples that build and pass their checks in CI | 100% of the catalog | CI end-to-end job |
| Re-applying an unchanged environment | No changes, under 10 s | CI timing |
| Resources left after destroy or a cancelled run | Zero | Conformance and end-to-end tests |
| Cold build of the benchmark environment | Under 90 s (goal, confirmed before v1 acceptance) | Benchmark job |
| Unit test coverage of spec, planner and checks | At least 80% of lines | `go test -cover` |

**Example catalog**

1. 1 control plane + 2 workers from plain kubeadm tasks (also the benchmark)
2. Two clusters on two networks, with a jump host holding both contexts
3. A cluster with no CNI
4. A cluster with Flannel
5. A fresh machine left for the learner to join
6. One task per kubeadm init phase, with a custom step between two phases
7. kube-proxy skipped
8. kubelet broken, confirmed by a wait task
9. Scheduler manifest broken, confirmed by a wait task
10. CNI config broken, confirmed by a wait task
11. A background task
12. A Helm chart installed from a tools machine
13. A host kubeconfig delivered by a transfer task
14. An environment with no checks
15. A package-based `kubeadm upgrade` to the next minor version, on the apt-managed node image

**Acceptance criteria**

- [ ] Every catalog example passes apply and check in CI on Linux; a manual run passes on a second Linux distribution.
- [ ] The section 10 walkthrough reproduces, including exit codes and report contents.
- [ ] Re-applying any unchanged example changes nothing; applying an edited manifest is refused unless recreate is requested.
- [ ] An interrupted apply stops within 10 s; the next apply rebuilds the environment and destroy leaves zero resources.
- [ ] A negative corpus of at least 30 invalid manifests each fails validation with the correct field path.
- [ ] Manifests that need VM-only capabilities fail planning with the capability named.
- [ ] The conformance suite, fake provider and JSON Schema are published in the repository.
- [ ] Cold build time is measured and recorded as the baseline for later releases.

## 10. Worked example: a troubleshooting environment

One file builds a cluster with three planted problems, and one command proves all three are present. The full manifest lives in `examples/tshoot-01/environment.yaml`; the excerpt shows only the parts that illustrate the concepts. Durations are illustrative.

**Story.** As a scenario author, I want a cluster where node01 has never joined, node02's kubelet is broken, and the `web` Service in namespace `shop` selects no pods, so that learners can practise joining a node, kubelet troubleshooting and Service debugging in one environment.

**Manifest excerpt**

```yaml
apiVersion: sim.dev/v1alpha2
kind: Environment
name: tshoot-01
backend: container

machines:
  - { names: [controlplane, node01, node02], image: kindest/node:v1.35.8 }
  - { name: runner, image: tools }

tasks:
  kubeadm-init:
    machine: controlplane
    run: kubeadm init --config /etc/kubeadm/init.yaml
    probes:
      - command: kubectl --kubeconfig /etc/kubernetes/admin.conf get --raw /readyz
  join-command:
    machine: controlplane
    needs: [kubeadm-init]
    run: kubeadm token create --print-join-command
  join-node02:                      # node01 gets no join task, so it stays fresh
    machine: node02
    needs: [join-command]
    run: "{{ tasks.join-command.stdout }}"
  # ... CNI, kubeconfig transfers and the shop app ...
  break-kubelet-ca:                 # deliberate breakage, proven by its probe
    machine: node02
    needs: [shop-app]
    run: |
      sed -i 's#^clientCAFile: .*#clientCAFile: /etc/kubernetes/pki/ca-old.crt#' /var/lib/kubelet/config.yaml
      systemctl restart kubelet
    probes:
      - command: grep -q 'ca-old.crt' /var/lib/kubelet/config.yaml
  node02-down:                      # wait task: no command, only a probe
    needs: [break-kubelet-ca]
    probes:
      - machine: runner
        command: kubectl get node node02 -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}'
        stdout: { matches: '^(False|Unknown)$' }

checks:
  node01-is-fresh:
    machine: node01
    run: test ! -f /etc/kubernetes/kubelet.conf
  web-has-no-endpoints:
    machine: runner
    run: kubectl -n shop get endpointslices -l kubernetes.io/service-name=web -o jsonpath='{.items[*].endpoints}'
    stdout: { equals: "" }
```

**Interaction**

1. `simctl validate -f environment.yaml` reports 4 machines, 4 files, 10 tasks and 6 checks, with no Docker needed.
2. `simctl plan -f environment.yaml` prints images pinned by digest, machine IPs and the 10 tasks in order. Nothing changes.
3. `simctl apply -f environment.yaml` builds the environment; checks do not run:

   ```text
    1 kubeadm-init on controlplane ............. ok  38.2s   probes 1/1
    4 join-node02 on node02 .................... ok  14.1s   probes 1/1
    9 break-kubelet-ca on node02 ............... ok   2.3s   probes 1/1
   10 node02-down .............................. ok  46.8s   probes 1/1 (Unknown)
   Environment tshoot-01 provisioned (6 checks defined; run them with simctl check)
   ```
4. `simctl check -f environment.yaml` runs the 6 checks, changes nothing, and exits 0 with `6/6 checks passed`.
5. Re-running `apply` reports `up to date`. Editing only a check still reports up to date. Editing a task is refused until the author asks to recreate.
6. `simctl destroy tshoot-01` removes the 4 machines, the network, the volumes and the state directory.

**Expected outcome**

| Aspect | Result |
| --- | --- |
| `kubectl get nodes` | `controlplane` Ready, `node02` NotReady, `node01` absent |
| node01 | Running, with containerd and kubeadm but no kubelet configuration |
| node02 | Joined; kubelet fails because its CA path points at a missing file |
| `shop/web` | Deployment running; Service has no endpoints |
| Reports and exit codes | Apply report status `provisioned`, exit 0; check report with every check passing, exit 0 |

## 11. Risks, assumptions and open questions

The largest risk is running kubeadm phase by phase inside `kindest/node` containers with custom names; it must be proven before any other Kubernetes work is built.

| Risk | Impact | Mitigation |
| --- | --- | --- |
| kubeadm misbehaves in kind node containers (entrypoint, cgroups, preflight checks) | Blocks every Kubernetes environment | Prove it first, reusing kind's container settings and kinder's phase handling. Falling back to the kind library is a scope change: it loses exam-style names (FR-11), phase-by-phase kubeadm (FR-12, example 6) and fresh machines to join (example 5), so it needs the owner's decision and an update to this PRD. |
| Manifests are long and repetitive; the tshoot-01 example is about 130 lines, roughly half of it a standard kubeadm bootstrap | The primary user's main job, authoring, becomes slow and error-prone | Accepted for v1: examples are copied and adapted. A reuse mechanism is planned for engine v2 |
| kubeadm flags or config fields change between Kubernetes minors | Examples break on upgrade | Images pinned by digest; the example catalog runs against every new image in CI |
| A task runs a host-affecting command and damages the user's machine | Loss of trust | FR-9 guard, documentation, and a clear statement that real isolation arrives with the VM backend |
| Linux hosts differ (cgroup v1 or v2, storage driver, kernel) | Platform-specific failures | Prerequisite check before every apply; conformance suite on two distributions before release |
| First runs download many images and are slow or fail offline | Poor first experience | Registry cache, pinned digests, an option to prefetch images |
| The manifest grows faster than its validation | Confusing errors | Negative test corpus and JSON Schema checked in CI from the start |

**Assumptions**

- `kindest/node` images for the pinned versions include kubeadm, kubelet, kubectl, containerd and the control-plane images, and run systemd as the init process.
- Users run Linux with Docker Engine and have at least 4 CPUs and 6 GB RAM free.
- v1 serves a single local user; no authentication or multi-tenancy.

**Open questions**

| Question | Decide when |
| --- | --- |
| Product name for the engine now that it stands alone, and the final API group (`sim.dev` is a placeholder) | Before the JSON Schema is published |
