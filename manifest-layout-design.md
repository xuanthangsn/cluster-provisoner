# Manifest Layout Design (v1alpha2)


## 1. Purpose and status

This document serves as the design of the manifest layout that will be used to provision a cluster 

## 2. Design principles

The manifest is as generic as possible and specializes only where Kubernetes provisioning needs it; when the two conflict, the CKA use case wins.

1. **Generic core.** Machines, networks, files and an ordered set of tasks describe any Linux environment; a Kubernetes cluster is just one possible result.
2. **No Kubernetes knowledge in the engine.** kubeadm, CNI installs, kubectl and Helm are commands run by tasks. Tasks come in two primitive types, `exec` and `transfer`; there are no built-in actions in Phase 1.
3. **Ordered, sequential tasks.** Order comes from `needs`, and tasks run one at a time. A step between two kubeadm phases is simply a task between two `kubeadm init phase …` tasks.
4. **Flat and shallow.** Five top-level sections of small objects, at most three levels deep.
5. **Explicit over implicit.** No hidden machines, no inferred dependencies, no start-time choices.
6. **Validated before anything runs.** Structure, references and backend capabilities are checked at `simctl plan`; command errors surface at run time and probes pinpoint them.

## 3. Layout at a glance

A manifest is a header plus five sections; only `machines` is required in practice.

```yaml
apiVersion: sim.dev/v1alpha2
kind: Environment
name: <env-name>
backend: container                 # container | vm

networks:                          # default: one auto-allocated network
  - { name: <net>, subnet: <cidr> }

machines:                          # Linux machines (containers or VMs)
  - name: <name>                   # or names: [a, b, c] for identical machines
    image: <oci-ref>               # or build: { context: ./dir, args: {...} }
    network: <net> | [<net>, ...]  # default: first network
    ip: <addr> | { <net>: <addr> }
    cpus: 2
    memory: 2Gi
    tags: [ ... ]                  # targets for files on: tag:<t>

files:                             # written before machines start
  - { path: /abs/path, content: "...", mode: "644", owner: root, on: [<machine>] }

tasks:                             # run one at a time by simctl apply (4.5)
  <task-name>:
    type: exec                     # exec (default) | transfer
    # exec
    machine: <machine>             # exactly one; required
    run: | ...                     # or script: ./file.sh args; none = wait task
    background: false
    exitCode: 0                    # criterion 1: the command's own result
    stdout: { contains: "..." }
    # transfer
    from: <machine>:<path>         # or a local file next to the manifest
    to: <machine>:<path>           # or state:<path> on the host
    mode: "600"
    # both
    probes:                        # criterion 2: commands re-run until they pass
      - { command: "...", machine: <machine>, interval: 2s, exitCode: 0, stdout: { equals: "..." } }
    timeout: 60s                   # window for one attempt
    attempts: 1
    needs: [<task>, ...]

checks:                            # run only by simctl check; always exec
  <check-name>:
    machine: ...
    run: | ...
    exitCode: 0                    # same two criteria as tasks
    probes: [ ... ]
```

**Outer lifecycle of `simctl apply`:**

1. Validate the whole manifest; everything is declared in the file, with no start-time choices.
2. Create networks.
3. Create machines and write `files` into them before they start.
4. Start machines; wait for each to be reachable.
5. Run the task graph until every task has succeeded or one fails.

`simctl check` runs the `checks` against the result at any time afterwards; it never changes anything.

## 4. Section reference

Every section is a flat list or map of small objects; the tables below list every field proposed for `v1alpha2`.

### 4.1 Header

| Field | Default | Notes |
| --- | --- | --- |
| `apiVersion` | required | `sim.dev/v1alpha2` |
| `kind` | required | `Environment` |
| `name` | required | DNS label, max 40 characters; prefixes all created resources |
| `backend` | `container` | `container` (Phase 1) or `vm` (Phase 3) |

### 4.2 `networks`

| Field | Default | Notes |
| --- | --- | --- |
| `name` | required | Unique; machines join by name |
| `subnet` | auto `/24` from `172.30.0.0/16` | Must not overlap other networks |

Without this section the engine creates one network named `default`.

### 4.3 `machines`

| Field | Default | Notes |
| --- | --- | --- |
| `name` / `names` | required | One machine, or several identical ones; becomes hostname and DNS name |
| `image` | required unless `build` | OCI reference or catalog alias (`kindest/node:v1.35.8`, `tools`, `ubuntu-24-04`) |
| `build` | none | `{ context, args }`; built once and cached |
| `network` | first network | A name, or a list for multi-homed machines |
| `ip` | auto | An address, or `{ <net>: <addr> }` when multi-homed |
| `cpus`, `memory` | backend default | Limits; on the container backend they do not change kubelet-reported capacity |
| `tags` | none | Free labels used for targeting (`files on: tag:worker`) |

### 4.4 `files`

| Field | Default | Notes |
| --- | --- | --- |
| `path` | required | Absolute; parent directories created |
| `content` / `source` | one required | Inline text, or a local path relative to the manifest |
| `mode`, `owner` | `"644"`, `root` | Octal string, user or user:group |
| `append` | `false` | Append instead of replace |
| `on` | all machines | Machine names or `tag:<t>` |

### 4.5 `tasks`

A map; the key is the task name and must be unique. Every task is one of two primitive types: `exec` runs a command on one machine, `transfer` copies one file. There are no built-in actions.

| Field | Used by | Default | Notes |
| --- | --- | --- | --- |
| `type` | — | `exec` | `exec` or `transfer` |
| `needs` | both | none | Tasks that must finish first |
| `machine` | exec | required | Exactly one machine name |
| `run` / `script` | exec | none | Inline bash, or a local script with arguments; at most one; none = a wait task |
| `background` | exec | `false` | Start the command and leave it running (rule 6) |
| `user`, `env` | exec | `root`, none | Probes inherit `env` |
| `exitCode` | exec, probe | `0` when there is a command; `0` for probes | A number, a list, or `nonzero` |
| `stdout` | exec, probe | none | One of `equals`, `contains`, `matches` (regex); trailing whitespace trimmed; add `2>&1` to match error text |
| `from` | transfer | required | `<machine>:<path>`, or a local file relative to the manifest |
| `to` | transfer | required | `<machine>:<path>`, or `state:<path>` in the host state directory |
| `mode`, `owner` | transfer | source mode, `root` | `owner` is ignored for `state:` |
| `probes` | both | none | List of `{ command, machine, interval, exitCode, stdout }` |
| `probes[].machine` | — | the task's machine | Required when the task has none |
| `probes[].interval` | — | `2s` | Re-runs continue after the probe passes |
| `timeout` | both | `60s` | Window for one attempt |
| `attempts` | both | `1` | Total attempts, including the first |
| `onFailure` | both | `fail` | `fail` or `ignore`; to require a failure, set `exitCode: nonzero` |

**Rules**

1. **Order.** Tasks run one at a time; the next is the first in declaration order whose `needs` are all done.
2. **Success.** Criterion 1: the command's `exitCode` and `stdout` match. Criterion 2: every probe passes. Both must hold. An `exec` task with no command is a wait task and needs at least one probe.
3. **Attempt.** Run the command once and check criterion 1. Meanwhile run every probe every `interval`, including after it passes; only its latest result counts.
4. **Outcome.** The attempt succeeds if, within `timeout`, criterion 1 holds and the latest result of every probe passes at the same moment; otherwise it fails, including when the command is still running.
5. **Retry.** After a failed attempt, if attempts remain, wait for the window to end and start again from rule 3; when none remain, `onFailure` decides.
6. **Background.** The command starts as the transient systemd unit `sim-<task>` and is not waited for; criterion 1 is not allowed, so probes decide. The unit lives with its machine, is stopped before a retry, and is bounded only by the machine's `cpus` and `memory`.
7. **Transfer.** Succeeds when the file exists at `to` with the requested mode; criterion 1 does not apply, and probes may add conditions.
8. **Templates.** `{{ tasks.<name>.stdout }}` and `{{ tasks.<name>.exitCode }}` of tasks in `needs`, and `{{ machines.<name>.ip }}`, may appear in commands, `env`, probes and transfer paths; they are filled in just before the task runs.

### 4.6 `checks`

Checks are always `exec`: they take the same fields as an `exec` task except `type`, `background` and `needs`, and pass by the same two criteria (4.5). They run only through `simctl check`, one at a time in declaration order, cannot read each other's output, and each ends `pass`, `fail` or `error`.

### 4.7 Validation rules

`simctl validate` and `simctl plan` reject a manifest that breaks any of these, naming the field.

1. Names are DNS-1123 labels within their length limits; machine names are unique in the environment, task and check names unique in their section.
2. Every image is a catalog alias or an OCI reference that the planner pins by digest; `build` contexts exist.
3. Network subnets do not overlap; static IPs fall inside their network's subnet; every network a machine names exists.
4. Every task, probe and check `machine` names exactly one declared machine; file `on` entries resolve to declared machines or `tag:<t>`; `exec` tasks with a command and all checks name a machine; probes of tasks without a machine name one.
5. A task's `type` is `exec` or `transfer`; `exec` tasks have at most one of `run` and `script` and no transfer fields; `transfer` tasks have `from` and `to` and no exec fields; an `exec` task without a command has at least one probe.
6. Every `needs` entry names a declared task, and the graph has no cycles.
7. Template references name tasks listed in that task's `needs`, or declared machines.
8. `transfer` endpoints name declared machines, a local file, or a `state:` path.
9. Every referenced local file (file sources, scripts, transfer sources) exists inside the environment directory.
10. Fields and commands that need capabilities the backend lacks are rejected with the capability named: VM-only features and known host-affecting commands (`swapon`, `swapoff`, `modprobe`, `date -s`, `sysctl -w kernel.*`, `hwclock`) on the container backend.
