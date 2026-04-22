# ADR-002: Instrumentation as a Top-Level Provider with Decomposed Resources

**Created**: 2026-04-17
**Status**: accepted
**Supersedes**: [docs/adrs/instrumentation/001-instrumentation-provider-design.md](001-instrumentation-provider-design.md)

## Context

gcx added instrumentation support under `gcx setup instrumentation` (ADR-001)
as a declarative onboarding flow. In use, two shortcomings emerged:

1. **Instrumentation is a product, not an onboarding step.** It has an
   ongoing management lifecycle (discover → configure → update → observe
   drift) and its own command tree. Nesting it under `setup` signals a
   one-shot wizard and hides it from the rest of the provider surface.
2. **It sits outside the declarative pipeline.** Every other Grafana
   resource gcx manages (dashboards, SLOs, fleet pipelines, …) can be
   `pull`ed, edited on disk, and `push`ed back via `gcx resources`.
   Instrumentation cannot. Its only declarative entry point is a bespoke
   `InstrumentationConfig` manifest applied through `gcx setup
   instrumentation apply`. For users who want "Grafana as code" — the
   point of the resources pipeline — this is a gap.

A secondary issue is the shape of `InstrumentationConfig` itself. It
bundles two independently-controlled surfaces into one document:

- Kubernetes monitoring flags for the cluster (cost metrics, energy
  metrics, cluster events, node logs).
- Per-namespace Beyla configuration (signal toggles plus per-workload
  overrides).

The underlying API is already split along this seam — separate
`Get`/`Set` endpoint pairs for each. The composite is a CLI artifact,
and it forces all-or-nothing updates where the API allows independent
ones.

The promotion of instrumentation to a top-level command is part of a
larger reorganization of the gcx CLI surface, but the choices this ADR
makes — resource decomposition, identity model, pipeline integration —
are load-bearing for the promotion regardless of what happens around it.

## Decision

**Promote instrumentation to a top-level `gcx instrumentation` provider,
replace the composite `InstrumentationConfig` manifest with two
independently-managed resource kinds, and integrate both into the
`gcx resources` declarative pipeline.**

### Resource model

Two kinds under `instrumentation.grafana.app/v1alpha1`:

- **`Cluster`** — the Kubernetes monitoring configuration for a
  connected cluster. One resource per cluster. Identity is the cluster
  name.
- **`App`** — the Beyla application-instrumentation configuration for a
  `(cluster, k8s-namespace)` pair. Many resources per cluster. Identity
  is composite and cross-cluster-unique.

**Example `Cluster`:**

```yaml
apiVersion: instrumentation.grafana.app/v1alpha1
kind: Cluster
metadata:
  name: my-cluster
spec:
  costMetrics: true
  clusterEvents: true
  energyMetrics: false
  nodeLogs: false
```

**Example `App`:**

```yaml
apiVersion: instrumentation.grafana.app/v1alpha1
kind: App
metadata:
  name: my-cluster-default      # ${cluster}-${namespace}, display-only
spec:
  cluster: my-cluster           # REQUIRED — authoritative
  namespace: default            # REQUIRED — authoritative
  selection: include
  tracing: true
  logging: true
  processMetrics: false
  extendedMetrics: false
  profiling: false
  apps:
    - name: my-service
      selection: include
      type: java
```

### App identity contract

- `metadata.name` is a **display key** generated as
  `${spec.cluster}-${spec.namespace}`. It is unique per Grafana stack and
  gives humans and tables a readable handle.
- `.spec.cluster` and `.spec.namespace` are **required and authoritative**.
  Writes use these fields exclusively; the provider does not parse
  `metadata.name` to derive identity.
- Any inconsistency between `metadata.name` and the spec fields on write
  is an error, not a warning. There are no best-effort fallbacks.

This mirrors the established codebase pattern of carrying authoritative
identifiers in `.spec` while using a generated display name in
`metadata.name` (e.g. fleet's `${slug}-${id}`).

### Command tree

```
gcx instrumentation clusters list
gcx instrumentation clusters get <name>
gcx instrumentation clusters create -f cluster.yaml
gcx instrumentation clusters update <name> -f cluster.yaml
gcx instrumentation clusters delete <name>

gcx instrumentation apps list [--cluster <name>]
gcx instrumentation apps get <name>
gcx instrumentation apps create -f app.yaml
gcx instrumentation apps update <name> -f app.yaml
gcx instrumentation apps delete <name>

gcx instrumentation setup <cluster>      # interactive bootstrap
gcx instrumentation discover <cluster>   # read-only workload scan
gcx instrumentation status               # per-cluster aggregate view
gcx instrumentation check <cluster>      # preflight validation
```

`gcx instrumentation setup` is a first-class entry point for users and
agents that know they want to configure instrumentation. How (or
whether) a future higher-level onboarding orchestrator invokes this
command is deferred — the command stands on its own.

### Pipeline integration

Both kinds register with the resources adapter pipeline. `gcx resources
pull` exports them alongside dashboards, SLOs, etc.; `gcx resources
push` upserts them with the same safety guarantees it offers for every
other kind (dry-run, summary output, concurrent bounded writes).

The write path for each kind preserves the optimistic-lock behaviour
introduced in ADR-001: a write is rejected if the remote state contains
items the local manifest doesn't. For `App` the lock now scopes to a
single namespace rather than the whole cluster — a narrower, more
informative failure mode.

Stack-derived backend URLs needed by the underlying API stay out of the
manifest and are injected by the provider at write time (preserving
ADR-001's constraint that URLs, instance IDs, and tokens never appear in
declarative manifests).

### `apply` verb retired

The former `gcx setup instrumentation apply -f …` command is removed.
Its responsibilities split cleanly across the new surface: imperative
`create`/`update` for single-resource changes, `gcx resources push` for
multi-resource declarative flows, `--dry-run` on each.

### Breaking change — no alias

`gcx setup instrumentation` and the `InstrumentationConfig` manifest
format are removed outright. No deprecation alias. Migration guidance
(how to split a legacy `InstrumentationConfig` into `Cluster` + `App`
manifests) goes into the changelog and provider reference.

### Rejected alternatives

- **Promote the commands without pipeline integration.** Simplest change
  but leaves the "instrumentation can't be managed as code" gap open and
  keeps the composite manifest.
- **Pipeline integration as read-only (pull, not push).** Asymmetric —
  `resources pull` works, `resources push` errors — which is a sharp
  edge to explain and document.
- **Keep the composite manifest under the new provider.** A single kind
  keyed by cluster preserves the existing all-or-nothing update
  semantics and couples two API surfaces that are otherwise independent.
- **Overload `metadata.namespace` as the cluster for `App`.** Mapped
  cleanly to Kubernetes idioms but diverged from the codebase-wide
  convention that `metadata.namespace` is the Grafana stack namespace.
- **Parse `metadata.name` to recover `App` identity.** Ambiguous
  whenever a cluster or k8s namespace contains a dash, and silent magic
  produces surprising failures. Rejected in favour of explicit required
  spec fields.

## Consequences

### Positive

- Instrumentation becomes a first-class product surface, discoverable
  from the top-level command list.
- "Grafana as code" works end-to-end for instrumentation: pull, edit,
  push, drift check — the same workflow users already apply to other
  resources.
- `App` and `Cluster` lifecycles are decoupled. Users can change Beyla
  namespace settings without touching cluster-wide monitoring flags,
  and vice versa.
- Update conflicts surface with narrower scope and clearer messages
  (per-namespace for `App`).

### Negative

- Hard break for existing `gcx setup instrumentation` users.
  Acceptable under preview status, but requires clear changelog
  communication and a migration note.
- Listing all `App` resources requires one API call per connected
  cluster — linear in cluster count. Bounded concurrency mitigates
  latency; worth flagging for stacks with many clusters.
- `App` writes perform read-modify-write against the full cluster
  payload. This is an API constraint, not a design choice, and it adds
  round-trip latency to every `create`/`update`/`delete`.

### Neutral / follow-up

- A migration helper that converts a legacy `InstrumentationConfig`
  manifest into one `Cluster` + N `App` manifests would smooth the
  transition. Out of scope here; candidate for a separate bead.
- The exact check-set for `gcx instrumentation check` (cluster
  reachability, agent presence, backend URL resolution, …) is an
  implementation detail to settle during build planning.
- A `Cluster` resource doesn't strictly create the cluster — clusters
  appear via agent registration. `create` here configures monitoring
  flags for a cluster name that may or may not yet have an agent.
  Analogous to how a Kubernetes `Node` is observed-then-configured,
  not created-then-observed. The nuance belongs in the CLI help text.
- If a higher-level setup orchestrator later needs to invoke
  `gcx instrumentation setup` from a guided flow, the integration
  contract can be designed at that point around the already-shipped
  command. This ADR does not constrain that choice.
