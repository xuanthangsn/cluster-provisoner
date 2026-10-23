# 19: Kubernetes node image support on the container provider

Status: ready-for-human

Type: HITL

## Parent

[PRD: Engine core — manifest to verified Environment](../PRD.md)

## What to build

Prove, and then support, FR-11 and FR-12 on the container provider: a Machine started from a Kubernetes node image must run systemd as its init process, have kubelet present as a service and containerd running, and support `kubeadm init`, `kubeadm join` and running kubeadm phase by phase — with the Machine's own name as its hostname and as its Kubernetes node name.

**This slice is HITL, and it is a spike before it is a feature.** `prd.md` section 11 names this the largest risk in the project: kubeadm may misbehave in these containers over the entrypoint, cgroups or preflight checks. If it cannot be made to work, the fallback to the kind library is a scope change that costs exam-style Machine names, phase-by-phase kubeadm and fresh Machines left for a learner to join. That needs the owner's decision and an update to `prd.md` — it is not something to work around quietly.

Note that this slice adds no Kubernetes knowledge to the engine. FR-13 still holds: a Machine stays a fresh Machine unless a Task bootstraps it, and the provider interface gains nothing Kubernetes-shaped. What changes is only what the container provider can host.

## Acceptance criteria

- [ ] A Machine from a pinned Kubernetes node image runs systemd as its init process
- [ ] kubelet is present as a service and containerd is running
- [ ] `kubeadm init` succeeds on a Machine whose name is an arbitrary DNS label, and the resulting Kubernetes node name is that Machine's name
- [ ] `kubeadm join` succeeds on a second Machine
- [ ] kubeadm can be run phase by phase, with an unrelated Task between two phases
- [ ] A Machine with no bootstrap Task stays a fresh Machine: no kubelet configuration and no node registration
- [ ] The engine still contains no Kubernetes-specific behaviour, asserted by a test over the engine's modules, and the provider interface gains nothing Kubernetes-shaped
- [ ] The container settings required to make this work are documented with their rationale
- [ ] Run as a build-tagged end-to-end test at seam S5
- [ ] If the approach cannot be made to work, the finding is written up with the fallback's cost and escalated for a scope decision rather than worked around

## Blocked by

- [02: Walking skeleton — `apply` and `destroy`](./02-skeleton-apply-destroy.md)
