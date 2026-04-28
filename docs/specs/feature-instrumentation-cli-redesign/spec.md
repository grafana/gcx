---
type: feature-spec
title: "gcx instrumentation CLI redesign: action verbs over Set/Get + observed state"
status: approved
research: docs/adrs/instrumentation/002-cli-redesign.md
created: 2026-04-27
---

# gcx instrumentation CLI redesign: action verbs over Set/Get + observed state

## Problem Statement

Grafana Cloud's Instrumentation Hub is backed by two fleet-management services: `instrumentation.v1.InstrumentationService` (per-cluster `Set/Get` on a configuration blob) and `discovery.v1.DiscoveryService` (collector-observed monitoring state with a built-in `PENDING_INSTRUMENTATION → INSTRUMENTED → PENDING_UNINSTRUMENTATION → NOT_INSTRUMENTED` state machine). The two services are joined server-side at read time, but exposed as RPCs — there is no per-resource CRUD, no `resourceVersion`, no `status` subresource, and no per-cluster delete RPC.

gcx today has no first-class surface for managing instrumentation. Operators who want to enable instrumentation on a Kubernetes cluster, configure namespace-level Beyla auto-instrumentation, or investigate why a workload isn't producing telemetry must use the Grafana Cloud collector app UI. That gap costs us the audience-A onboarding path (one-command "instrument this cluster end-to-end") and the audience-C investigation path (granular per-workload observed state, `services list --status=ERROR`-style fleet sweeps).

The design challenge is shape mismatch. The natural mental model — `kind: Cluster`, `kind: App`, declarative `apply -f`, `gcx resources push/pull` round-trip — does not survive contact with a backend that has no `resourceVersion`, no per-resource delete, no server-side tombstone, and no list-of-configured-clusters RPC. A CRUD facade over `Set/Get` either lies about its guarantees (declared state stored, observed state queried, with no overlap window) or blocks on backend evolution that is not on the roadmap. Internal experimentation confirmed both failure modes empirically: a CRD facade produced bug-shaped symptoms (apps get not-found right after push, list/status disagreeing with get, delete having no effect because Alloy re-registers) that are manifestations of one design mismatch, not independent defects.

This spec adopts an action-verb command tree grounded in the actual API shape: imperative verbs on a cluster/app/service hierarchy, observed state as primary for "what is the system doing", declared state via a separate read path for "what did I configure", no `gcx resources` integration (deferred until the backend grows real CRUD primitives).

The audiences are locked from prior brainstorming:

- **Primary — A**, day-1 onboarding operator: "I have a new cluster, I want it instrumented end-to-end." Pulls `setup`, action verbs, `wait` for state-machine transitions.
- **Secondary — C**, day-N investigator/SRE: "An app isn't producing telemetry — show me what Beyla sees." Pulls `status`, `services list --status=ERROR`, granular per-workload reads.
- **Deferred — B**, GitOps/platform engineer: listed for completeness; does not constrain this design and is blocked on backend evolution.

## Scope

### In Scope

- Replacing the `gcx instrumentation` command surface with an action-verb command tree (`setup`, `status`, `clusters`, `clusters apps`, `services`).
- Rewriting `internal/providers/instrumentation/types.go` with corrected backend types (drop `App.Selection`, add `App.Autoinstrument`, add observed status fields, retain `Cluster.Selection` for serialization correctness, remove `metadata.name` enforcement on `App`).
- Returning an empty `Registrations()` slice from the `instrumentation` provider so no GVK is registered with the resource adapter.
- Deleting `observability/instrumentation/cluster.yaml` and `observability/instrumentation/apps.yaml` schema files.
- Splitting read paths by intent: `get`/`list` go through declared-state endpoints (`GetK8SInstrumentation`, `GetAppInstrumentation`); `status`/`wait` go through observed-state endpoints (`RunK8sMonitoring`, `RunK8sDiscovery`).
- Implementing tri-state flag semantics on `enable` (`--flag` / `--no-flag` / unset → preserve) with read-modify-write per-namespace (apps) or per-cluster (clusters) granularity.
- Implementing destructive verbs (`disable`, `reset`) gated on `--yes`.
- Implementing `wait` polling at 5s intervals matching the UI cadence, default 5m timeout, exit 0 on transition out of `PENDING_*`.
- Implementing `setup` as a loud, mutating, idempotent onboarding command that calls `SetupK8sDiscovery`, prompts for K8s flags (or applies defaults under `--yes`), calls `SetK8SInstrumentation` if anything changed, and prints a parameterized helm command on stdout with a mutation summary on stderr.
- Implementing client-side optimistic-lock comparison on read-modify-write paths with error messages naming the conflicting namespace and the detected change.
- Bare-domain-type JSON/YAML output (no K8s envelope, no `kind:`/`apiVersion:` wrapper) — explicit divergence from D9 Contract 2.
- Output contracts following D9 Contract 1: `NAME` first, `STATUS` second-to-last, `AGE` last; STATUS normalized to `OK`/`FAILING`/`NODATA` per Contract 3 with the underlying state-machine value in JSON/YAML and `wide` output.
- `services include`/`exclude`/`clear` DWIM operations on a single workload via `Get` + targeted `apps[]` mutation + `SetAppInstrumentation`.
- A PR-body design-rationale section (and CHANGELOG entry) covering the action-verb command tree, the no-`gcx resources` integration position, and the audience-A / audience-C path narrative.

### Out of Scope

- **GitOps / declarative file-driven workflows.** The audience for `apply -f instrumentation.yaml` and round-trip `resources pull` is real but blocked on backend evolution (true CRUD endpoints, resource versions, real delete semantics). Out of scope until the fleet-management API grows those primitives.
- **Embedded helm install execution.** `setup` continues to print a helm command; running `helm install` on the user's behalf is tracked in #546 (access policy auto-mint + helm exec) and out of scope here.
- **Hand-written OpenTelemetry SDK instrumentation.** This spec covers Beyla auto-instrumentation only — the surface managed by `instrumentation.v1.InstrumentationService`. Manual SDK patterns are unrelated.
- **KUBECONFIG auto-detection** in `setup`. Cluster name is positional; auto-detection from the active KUBECONFIG context is deferred future work.
- **Per-workload override flags** on `services include` (e.g., custom autoinstrument fields). Current scope is binary include/exclude/clear; richer overrides are deferred.
- **Server-side optimistic locking.** The backend is unversioned (last-writer-wins); the client-side comparison is a UX improvement, not a transactional guarantee.
- **`gcx resources push/pull/get/delete` integration** for instrumentation kinds. Re-entered when the backend supports it.

## Key Decisions

| Decision | Chosen | Rationale | Source |
|---|---|---|---|
| Surface shape | Action verbs (`enable`/`disable`/`reset`/`setup`/`include`/`exclude`/`clear`/`wait`) on cluster/app/service hierarchy | Matches the actual `Set/Get` + observed-state backend; the alternative (CRUD facade) was empirically shown to produce abstraction-leak bug shapes during internal experimentation | ADR-016 §Decision |
| Declarative CRUD (Design A) | Rejected | Backend has no `resourceVersion`, no per-resource delete, no server-side tombstone, no list-of-configured-clusters RPC; would require client-side simulation that lies about its guarantees, or backend redesign not on roadmap | ADR-016 §Rejected alternatives |
| Tactical CRUD facade with targeted fixes | Rejected | The bug shape from internal experimentation was a manifestation of one design mismatch, not independent defects; tactical fixes would keep producing indistinguishable bug reports | ADR-016 §Rejected alternatives |
| Hybrid (CRD kinds + imperative verbs side by side) | Rejected | The `gcx resources` integration cannot be made coherent against this backend; a half-working second path is worse than none and divides command-discovery effort | ADR-016 §Rejected alternatives |
| `gcx setup instrumentation` framework from ADR-014 | Superseded | Manifest workflow hits the same backend blockers as Design A; multi-product `setup` framework did not materialize; audience-A is better served by a single top-level wizard | ADR-016 §Context, supersedes ADR-014 |
| GVK registration for instrumentation kinds | None — `Registrations()` returns empty slice | No coherent CRUD semantics over `Set/Get`; nothing for `gcx resources` to honor | ADR-016 §Adapter registry change |
| `services` placement | Top-level under `gcx instrumentation`, not nested under `clusters` | `RunK8sDiscovery` is a single fleet-wide RPC; services are not stored entities. Top-level placement honors API shape and preserves audience-C "find broken services across the fleet" path | ADR-016 §Tree placement rationale |
| `clusters apps` placement | Nested under `clusters` | `App` namespace entries are stored as rows inside the per-cluster blob (`GetAppInstrumentation`/`SetAppInstrumentation`); nesting mirrors storage shape | ADR-016 §Tree placement rationale |
| `status` placement | Cross-cutting, drills via flags (`--cluster`, `--namespace`) rather than verb nesting | Consistent with D9 "`status` always reads observed state at any level" contract | ADR-016 §Tree placement rationale |
| Read-path split | `get`/`list` → declared (`GetK8SInstrumentation`/`GetAppInstrumentation`); `status`/`wait` → observed (`RunK8sMonitoring`/`RunK8sDiscovery`) | Both questions are real ("what did I configure?" vs "what is Beyla seeing?"); collapsing them into a single read verb introduces a race between declared-state writes and observed-state reads | ADR-016 §Read paths split by intent |
| `enable` flag semantics | Tri-state (`--flag` / `--no-flag` / unset → preserve), RMW with optimistic-lock check | Preserves unmodified per-namespace state under concurrent edits; the explicit error format on conflict names the namespace and the detected diff for actionable user feedback | ADR-016 §Verb semantics |
| `disable` semantics for clusters | `SetK8SInstrumentation(Selection=EXCLUDED)` → backend deletes pipeline; cluster naturally drops from `list` once collector stops reporting | Backend has no per-cluster delete RPC; no synthetic tombstone | ADR-016 §Verb semantics; legacy draft §Backend ground truth |
| `disable` semantics for apps | Remove namespace from `namespaces[]` via `SetAppInstrumentation`; if no namespace has included content the backend deletes the app pipeline | Whole-list replace is the only write primitive | ADR-016 §Verb semantics; legacy draft (k8s_beyla.go:292-314) |
| `reset` semantics | Restore defaults (end state: instrumented with defaults, not "not instrumented") | Distinct from `disable`; matches collector-app UI affordance | ADR-016 §Verb semantics |
| `wait` poll cadence | 5s intervals; default 5m timeout | Matches collector-app UI polling cadence | ADR-016 §Verb semantics |
| `App.Selection` field | Dropped from internal type | Dead field — `pkg/instrumentation/v1/k8s_beyla.go:291` confirms backend ignores `AppNamespace.selection` | ADR-016 §Internal types |
| `App.Autoinstrument` field | Added to internal type; `enable` always sets it true | Actual on/off knob for namespace-level instrumentation that drives the backend's pipeline-create path | ADR-016 §Internal types |
| `Cluster.Selection` field | Retained in type for serialization correctness; not user-controllable; visible in JSON/YAML; hidden in table/wide output (redundant with STATUS) | Real backend field; needed for round-trip in RMW writes | ADR-016 §Internal types |
| `metadata.name` enforcement on App | Removed (`validateAppIdentity` deleted) | App identity is `(cluster, namespace)` positionally on every command | ADR-016 §Internal types |
| Output envelope | Bare domain type (no `kind:`/`apiVersion:`) — divergence from D9 Contract 2 | Resources are not registered with the adapter and not addressable via `gcx resources`; envelope exists to support round-tripping which we are explicitly not supporting | ADR-016 §Output contracts |
| STATUS normalization | `OK`/`FAILING`/`NODATA` per D9 Contract 3, with underlying proto enum in JSON/YAML/wide | Cross-provider parity in human-facing output; full fidelity in machine output | ADR-016 §Output contracts |
| Schema files | No `cluster.yaml` / `apps.yaml` files under `observability/instrumentation/` | No GVK registration → no schema | ADR-016 §Migration |

## Backend ground truth (verified)

The redesign is grounded in a read-only investigation of the `grafana/fleet-management` source. Citations are file:line in that repo and are reproduced here so reviewers and implementers can verify the requirements below against the same evidence the spec was derived from.

### Read endpoints

| Endpoint | Returns | Behavior on never-configured cluster |
|---|---|---|
| `GetK8SInstrumentation(cluster)` | `K8SCluster{name, costmetrics, energymetrics, clusterevents, nodelogs, selection}` | HTTP 200 with bare object, only `name` set (`pkg/instrumentation/v1/k8s_mon.go:121-154`) |
| `GetAppInstrumentation(cluster)` | `AppCluster{name, namespaces[]}` | HTTP 200 with empty `namespaces` (`pkg/instrumentation/v1/k8s_beyla.go:350-385`) |
| `RunK8sMonitoring()` | `K8sMonitoringCluster[]` with state-machine `instrumentation_status` per cluster + per namespace | Cluster set is built from `survey_info{}` Prometheus query (`pkg/discovery/v1/prometheus.go:204-306`); the state-machine join (`pkg/discovery/v1/http.go:234-275`) only runs over clusters already in that set. **Clusters that have been `Set` but whose Alloy collector has not started reporting `survey_info` are invisible to this RPC** — they require the `ListPipelines` fallback (see "Things the backend does NOT have" below). For clusters that ARE in the iteration, `PENDING_INSTRUMENTATION` is emitted when `survey_info` is present but `kube_node_info{cluster}` is not (`pkg/discovery/v1/http.go:264-268`). |
| `RunK8sDiscovery()` | `DiscoveryItem[]` per workload with state-machine `instrumentation_status` | Joins `survey_info{}` (Beyla discovery) + `target_info{}` (instrumented services) + stored app pipeline state (`pkg/discovery/v1/http.go:277-331`) |

### Write endpoints

| Endpoint | Effect |
|---|---|
| `SetK8SInstrumentation(cluster, flags, urls)` | If `Selection != SELECTION_INCLUDED` → **deletes** the K8s monitoring pipeline (`pkg/instrumentation/v1/k8s_mon.go:70`, `:86-91`). Otherwise upserts with the requested flags. |
| `SetAppInstrumentation(cluster, namespaces[], urls)` | Whole-list replace of the cluster's app config. If no namespace has any included content → **deletes** the app pipeline (`pkg/instrumentation/v1/k8s_beyla.go:292-314`). |
| `SetupK8sDiscovery(urls)` | Idempotent: if a Beyla survey pipeline exists, returns success without modification (`pkg/discovery/v1/http.go:46-86`). Per-tenant scope. |

### State machine (observed via DiscoveryService)

```
                       SetK8S/SetApp(INCLUDED)
NOT_INSTRUMENTED ──────────────────────────────► PENDING_INSTRUMENTATION
       ▲                                                     │
       │                                                     │ collector reports
       │ collector stops reporting                           │ (kube_node_info / target_info)
       │                                                     ▼
PENDING_UNINSTRUMENTATION ◄────────────────────── INSTRUMENTED
                          SetK8S/SetApp(EXCLUDED
                          or removed entry)

EXCLUDED — set by user via Selection=SELECTION_EXCLUDED, kept visible (no PENDING_EXCLUSION transition)
ERROR    — defined in proto; never set by current backend code (reserved)
```

Transitions are reactive (Prometheus metric presence/absence at query time), not time-based. There is no server-side stale-data timeout — every read of `RunK8sMonitoring`/`RunK8sDiscovery` re-queries Prometheus.

### Things the backend does NOT have

- **No "list configured clusters" RPC** scoped to `InstrumentationService`. The required fallback is `PipelineService.ListPipelines` filtered by K8s monitoring pipeline metadata: `clusters list` MUST merge its results with `RunK8sMonitoring()` so that any cluster with a stored K8s monitoring pipeline but no `survey_info` reporting yet appears with status `PENDING_INSTRUMENTATION` (Open Question 1, RESOLVED — see ground-truth note above and FR-018, FR-019).
- **No server-side optimistic locking.** Last-writer-wins on transactional MySQL but unversioned (`pkg/storage/primary/mysql/pipeline_storage.go:219-255`).
- **No "delete cluster" RPC.** Disable = `Set` with `Selection=EXCLUDED`, which deletes the pipeline; once Alloy stops reporting, the cluster naturally drops out of `RunK8sMonitoring`.
- **Namespace-level `Selection` is unused** (TODO at `pkg/instrumentation/v1/k8s_beyla.go:291`). The actual namespace control surface is `AppNamespace.autoinstrument`. Per-app `Selection` (inside `AppNamespace.apps[]`) IS used by the backend — it is the per-workload override knob driven by `services include/exclude/clear`.
- **No collector-side tombstone signal.** Collectors poll `GetConfig` at their own cadence; "stop instrumenting" propagates only when collectors observe the pipeline is gone.

## Functional Requirements

### Command tree

- **FR-001**: The CLI MUST expose `gcx instrumentation` as a top-level command with subcommands: `setup`, `status`, `clusters`, `services`.
- **FR-002**: `gcx instrumentation clusters` MUST expose subcommands: `list`, `get`, `enable`, `disable`, `reset`, `wait`, `apps`.
- **FR-003**: `gcx instrumentation clusters apps` MUST expose subcommands: `list`, `get`, `enable`, `disable`, `reset`, `wait`.
- **FR-004**: `gcx instrumentation services` MUST expose subcommands: `list`, `get`, `include`, `exclude`, `clear`.
- **FR-005**: `gcx instrumentation --help` MUST list exactly the subcommands `clusters`, `services`, `setup`, `status` (plus the standard `help` subcommand). No other top-level subcommand under `instrumentation` MUST appear; in particular, no `create`/`update`/`delete` siblings of `clusters`/`services` MUST exist (those operations are reached via `enable`/`disable`/`reset` per FR-026..FR-034).

### Adapter registry and resource integration

- **FR-006**: The `instrumentation` provider's `Registrations()` MUST return an empty slice; no GVK is registered.
- **FR-007**: `gcx resources schemas` MUST NOT list any kind under `instrumentation.grafana.app`.
- **FR-008**: `gcx resources push -p observability/instrumentation` MUST reject any document with `apiVersion: instrumentation.grafana.app/v1alpha1` with an error message of the form `unknown kind: Cluster` or `unknown kind: App`.
- **FR-009**: The repository MUST NOT contain `observability/instrumentation/cluster.yaml` or `observability/instrumentation/apps.yaml`.
- **FR-010**: `wire/wire.go` MUST register the `gcx instrumentation` command tree but MUST NOT wire the instrumentation provider into the resource adapter pipeline.

### Internal types

- **FR-011**: `internal/providers/instrumentation/types.go` MUST NOT contain a `Selection` field on the `App` type.
- **FR-012**: `internal/providers/instrumentation/types.go` MUST contain an `Autoinstrument bool` field on the `App` type.
- **FR-013**: `internal/providers/instrumentation/types.go` MUST retain a `Selection` field on the `Cluster` type for serialization correctness; this field MUST NOT be user-controllable via flags.
- **FR-014**: The `Cluster.Selection` field MUST appear in JSON and YAML output; it MUST NOT appear in `text` or `wide` table output (redundant with STATUS).
- **FR-015**: The internal types MUST include observed-status fields populated from `RunK8sMonitoring` for table and wide output, using the proto enum directly rather than freeform strings.
- **FR-016**: The instrumentation provider MUST NOT enforce `metadata.name` on `App` types; the `validateAppIdentity` function MUST be removed.
- **FR-017**: App identity in all commands MUST be positional `(cluster, namespace)`; no command argument MAY require a `metadata.name` field on App.

### Read paths (declared state)

- **FR-018**: `gcx instrumentation clusters list` MUST enumerate clusters by merging two sources: (a) `RunK8sMonitoring()` (observed clusters with state-machine status), and (b) `PipelineService.ListPipelines()` filtered by K8s monitoring pipeline metadata (clusters that have been `Set` but may not yet be reporting `survey_info`). Any cluster present only in source (b) — i.e., a stored pipeline with no observed reporting — MUST appear in the output with status `PENDING_INSTRUMENTATION`. After enumeration, the command MUST fan out `GetK8SInstrumentation` per cluster in parallel, capped at 10 concurrent requests, to populate declared-state fields.
- **FR-019**: `gcx instrumentation clusters get <cluster>` MUST call `GetK8SInstrumentation(cluster)` and cross-reference observed status from a single `RunK8sMonitoring()` call. If `<cluster>` is absent from the `RunK8sMonitoring()` response (i.e., not yet reporting `survey_info`), the command MUST consult `PipelineService.ListPipelines()` filtered by K8s monitoring pipeline metadata to determine status: pipeline present → `PENDING_INSTRUMENTATION`; pipeline absent → `NOT_INSTRUMENTED`.
- **FR-020**: `gcx instrumentation clusters apps list <cluster>` MUST call `GetAppInstrumentation(cluster)` (single RPC) and return all namespace entries.
- **FR-021**: `gcx instrumentation clusters apps get <cluster> <namespace>` MUST call `GetAppInstrumentation(cluster)` and filter client-side to the requested namespace; if the namespace is absent the command MUST emit a CLI-level not-found error.

### Read paths (observed state)

- **FR-022**: `gcx instrumentation services list` MUST call `RunK8sDiscovery()` (single fleet-wide RPC) and apply `--cluster`, `--namespace`, `--status`, `--all` filters client-side.
- **FR-023**: `gcx instrumentation services get <cluster> <namespace> <service>` MUST call `RunK8sDiscovery()` and filter to the requested workload.
- **FR-024**: `gcx instrumentation status` MUST present the same cluster set as `clusters list` (per FR-018, including the `PipelineService.ListPipelines` merge for pre-Alloy clusters), reporting state-machine status from `RunK8sMonitoring()` for clusters with observed state and `PENDING_INSTRUMENTATION` for clusters present only in pipeline state. The command MUST additionally call `RunK8sDiscovery()` when drilling to a namespace via `--namespace`.
- **FR-025**: `gcx instrumentation status` MUST accept `--cluster <name>` and `--namespace <ns>` flags to narrow the observed view.

### Write paths

- **FR-026**: `gcx instrumentation clusters enable <cluster> [flag-mods]` MUST perform a read-modify-write cycle: `GetK8SInstrumentation(cluster)` → apply tri-state flag mutations → `SetK8SInstrumentation`.
- **FR-027**: Tri-state flag semantics: `--<flag>` MUST set the flag to true; `--no-<flag>` MUST set the flag to false; an unset flag MUST preserve the existing value.
- **FR-028**: `gcx instrumentation clusters apps enable <cluster> <namespace> [flag-mods]` MUST perform a read-modify-write cycle on the namespace entry only: existing per-workload `apps[]` overrides and other namespaces' entries MUST be preserved.
- **FR-029**: `gcx instrumentation clusters apps enable` MUST always set `Autoinstrument` to true on the target namespace entry.
- **FR-030**: All read-modify-write commands MUST perform a client-side optimistic-lock comparison between the value read at the start of the cycle and the value re-read immediately before write; if the values differ, the command MUST exit non-zero with an error message naming the conflicting namespace (where applicable) and the detected change.
- **FR-031**: `gcx instrumentation clusters disable <cluster>` MUST call `SetK8SInstrumentation(cluster, Selection=SELECTION_EXCLUDED)`, and MUST require `--yes` to proceed.
- **FR-032**: `gcx instrumentation clusters apps disable <cluster> <namespace>` MUST remove the namespace entry from the cluster's `namespaces[]` list via `SetAppInstrumentation`, and MUST require `--yes` to proceed.
- **FR-033**: `gcx instrumentation clusters reset <cluster>` MUST restore default flags via `SetK8SInstrumentation` and MUST require `--yes` to proceed; the end state is "instrumented with defaults", distinct from `disable`. Operationally, `clusters reset` is equivalent to a fresh `setup` mutation MINUS the helm-hint emission and `SetupK8sDiscovery` bootstrap.
- **FR-034**: `gcx instrumentation clusters apps reset <cluster> <namespace>` MUST replace the namespace entry with `{autoinstrument: true, all standard flags on, apps: []}` (per-workload overrides MUST be wiped), and MUST require `--yes` to proceed.
- **FR-035**: `gcx instrumentation services include <cluster> <namespace> <service>` MUST `GetAppInstrumentation(cluster)`, mutate the targeted entry inside the namespace's `apps[]` (ensuring instrumented), and call `SetAppInstrumentation` with optimistic-lock guard; the operation MUST be idempotent. The mutation MUST remove any existing `EXCLUDED` override on the workload AND MUST add an `INCLUDED` override iff the namespace has `autoinstrument=false`; otherwise no override MUST be added (no redundant overrides).
- **FR-036**: `gcx instrumentation services exclude <cluster> <namespace> <service>` MUST perform the symmetric operation, ensuring the workload is NOT instrumented; the operation MUST be idempotent. The mutation MUST remove any existing `INCLUDED` override on the workload AND MUST add an `EXCLUDED` override iff the namespace has `autoinstrument=true`; otherwise no override MUST be added (no redundant overrides).
- **FR-037**: `gcx instrumentation services clear <cluster> <namespace> <service>` MUST remove any per-workload override, falling back to the namespace default; the operation MUST be idempotent.

### Setup wizard

- **FR-038**: `gcx instrumentation setup <cluster>` MUST call `SetupK8sDiscovery` (server-side idempotent — no-op if a Beyla survey pipeline already exists).
- **FR-039**: `gcx instrumentation setup <cluster>` MUST prompt the user for K8s flags interactively when stdin is a TTY and `--yes` is not set. Interactive prompts MUST default to the recommended flag set if the cluster is currently unconfigured, OR to the cluster's current declared flag values if it is already configured.
- **FR-040**: `gcx instrumentation setup <cluster> --yes` MUST apply default K8s flags without prompting. The default flag values MUST be `costMetrics=true, clusterEvents=true, energyMetrics=false, nodeLogs=false`. Per-flag overrides supplied alongside `--yes` (`--cost`/`--no-cost`, `--events`/`--no-events`, `--energy`/`--no-energy`, `--logs`/`--no-logs`) MUST take precedence over these defaults.
- **FR-041**: `gcx instrumentation setup <cluster>` MUST call `SetK8SInstrumentation` only when at least one flag value differs from the current declared state.
- **FR-042**: `gcx instrumentation setup <cluster>` MUST print a parameterized helm command to stdout.
- **FR-043**: `gcx instrumentation setup <cluster>` MUST emit a mutation summary to stderr listing every mutation it performed (server-side calls + flag changes); the summary MUST be loud enough that re-running with no changes produces a "no changes" line.
- **FR-044**: `gcx instrumentation setup <cluster> --print-helm-only` MUST print the helm command and MUST NOT mutate any cluster state (no `SetupK8sDiscovery`, no `SetK8SInstrumentation`).
- **FR-045**: Re-running `gcx instrumentation setup <cluster>` with identical inputs MUST be idempotent (no mutations, summary line indicating no changes).
- **FR-045a**: `gcx instrumentation setup <cluster>` MUST expose the flag inventory: `--yes`, `--cost`/`--no-cost` (controls `costMetrics`), `--events`/`--no-events` (controls `clusterEvents`), `--energy`/`--no-energy` (controls `energyMetrics`), `--logs`/`--no-logs` (controls `nodeLogs`), and `--print-helm-only`. Resolution precedence under `--yes`: any per-flag override (`--cost`, `--no-cost`, etc.) MUST take precedence over the FR-040 defaults; flags with no override MUST take the FR-040 default value (NOT the prior cluster value — `setup` is "apply this configuration", not RMW-preserve like `enable`). Idempotence (FR-045) holds iff the cluster's prior state already matches the resolved flag set.

### Wait

- **FR-046**: `gcx instrumentation clusters wait <cluster>` MUST poll `RunK8sMonitoring()` at 5-second intervals. While `<cluster>` is absent from the `RunK8sMonitoring()` response, the poller MUST treat the cluster as `PENDING_INSTRUMENTATION` and continue polling (covering the pre-Alloy window — see FR-018, FR-019). The command MUST exit 0 when the cluster's `instrumentation_status` transitions to `INSTRUMENTED` (or any terminal non-error state); this is subject to the carve-out in FR-049 for terminal error states. Before the first poll, the command SHOULD verify a K8s monitoring pipeline exists (via `GetK8SInstrumentation` or `PipelineService.ListPipelines`) and fail-fast if no pipeline is found, rather than polling to timeout.
- **FR-047**: `gcx instrumentation clusters apps wait <cluster> <namespace>` MUST poll `RunK8sDiscovery()` (or `RunK8sMonitoring` with namespace filter, per implementation) at 5-second intervals and exit 0 when the namespace's `instrumentation_status` transitions to `INSTRUMENTED` (or any terminal non-error state); this is subject to the carve-out in FR-049 for terminal error states.
- **FR-048**: All `wait` commands MUST accept a `--timeout` flag with default `5m`; on timeout, the command MUST exit non-zero.
- **FR-049**: All `wait` commands MUST exit non-zero when terminal `INSTRUMENTATION_ERROR` state is observed.

### Output contracts

- **FR-050**: Table output for clusters and apps MUST place `NAME` in the first column, `STATUS` in the second-to-last column, and `AGE` in the last column (D9 Contract 1).
- **FR-051**: Human-facing table output STATUS values MUST be normalized to one of `OK`, `FAILING`, `NODATA` (D9 Contract 3).
- **FR-052**: JSON, YAML, and `wide` table output MUST expose the underlying state-machine value (proto enum: `INSTRUMENTED`, `PENDING_INSTRUMENTATION`, `PENDING_UNINSTRUMENTATION`, `NOT_INSTRUMENTED`, `EXCLUDED`, `INSTRUMENTATION_ERROR`).
- **FR-053**: JSON and YAML output MUST be the bare domain type — no K8s envelope, no `kind:` field, no `apiVersion:` field.
- **FR-054**: `services list --status=ERROR` MUST filter to only services with terminal error state (audience-C "find broken services" path).
- **FR-054a**: Default and `--output=wide` table column inventories MUST be:

  | Resource | Default columns | Wide adds |
  |---|---|---|
  | Cluster | `NAME` `NAMESPACES` `WORKLOADS` `PODS` `STATUS` `AGE` | `COST` `EVENTS` `ENERGY` `LOGS` `NODES` |
  | App (namespace) | `NAME` `CLUSTER` `WORKLOADS` `PODS` `AUTOINSTRUMENT` `STATUS` `AGE` | `TRACING` `LOGGING` `PROCESS_METRICS` `EXTENDED_METRICS` `PROFILING` `OVERRIDES` |
  | Service | `NAME` `CLUSTER` `NAMESPACE` `TYPE` `LANG` `STATUS` `AGE` | `WORKLOAD_TYPE` `OS` `INSTRUMENTATION_ERROR` |

## Acceptance Criteria

### Resource-adapter integration (verbs only — no GVK)

- **AC-001**:
  GIVEN the `instrumentation` provider is built into `bin/gcx`
  WHEN the operator runs `gcx resources schemas`
  THEN no kind under the `instrumentation.grafana.app` group MUST appear in the output (verified by exit non-zero of `... | grep instrumentation\.grafana\.app`).

- **AC-002**:
  GIVEN a YAML file with `apiVersion: instrumentation.grafana.app/v1alpha1` and `kind: Cluster` (or `kind: App`)
  WHEN the operator runs `gcx resources push -f cluster.yaml`
  THEN the command MUST exit non-zero with an error message containing `unknown kind: Cluster` (or `unknown kind: App`).

### Declared-state read-after-write parity

- **AC-003**:
  GIVEN a cluster `c1` with no app instrumentation configured
  WHEN the operator runs `gcx instrumentation clusters apps enable c1 grotshop` and immediately afterwards `gcx instrumentation clusters apps get c1 grotshop`
  THEN the second command MUST exit 0 within 1 second AND the output MUST contain `name: grotshop` AND `autoinstrument: true`. (No race between the declared-state write and the declared-state read.)

### Setup wizard mutation visibility

- **AC-004**:
  GIVEN a cluster `c1` with existing instrumentation configuration
  WHEN the operator runs `gcx instrumentation setup c1 --yes`
  THEN stderr MUST contain a mutation summary listing every server call and flag change performed
  AND stdout MUST contain a parameterized helm command.

- **AC-005**:
  GIVEN a cluster `c1` with existing instrumentation configuration captured to file `before.yaml` via `gcx instrumentation clusters get c1 -o yaml`
  WHEN the operator runs `gcx instrumentation setup c1 --print-helm-only`
  AND captures `gcx instrumentation clusters get c1 -o yaml` to `after.yaml`
  THEN `before.yaml` MUST equal `after.yaml` (no cluster spec mutation occurred).

### Optimistic-lock conflict messaging

- **AC-006**:
  GIVEN two concurrent operators executing `gcx instrumentation clusters apps enable c1 grotshop --tracing` and `gcx instrumentation clusters apps enable c1 grotshop --no-tracing` against the same cluster
  WHEN the second `Set` call lands after the first
  THEN one of the operators MUST receive an error message that follows the form:
  `apps enable: cannot write — namespace "{name}" was modified concurrently ({summary of change}). Re-fetch and retry.`
  where `{name}` is the conflicting namespace (`grotshop` in this case) and `{summary of change}` describes the detected diff (e.g., `tracing: true → false`, `added apps[]: frontend`, etc.).

### List / get / status agreement

- **AC-007**:
  GIVEN a cluster `c1` newly enabled via `gcx instrumentation clusters enable c1`
  WHEN the operator runs `gcx instrumentation clusters list`, `gcx instrumentation clusters get c1`, and `gcx instrumentation status` in any order
  THEN all three commands MUST agree on cluster existence
  AND `c1` MUST appear with status `PENDING_INSTRUMENTATION` (in JSON/YAML/wide) until the Alloy collector reports.

### Beyla pipeline produced on app enable

- **AC-008**:
  GIVEN a cluster `c1` with Alloy reporting and namespace `grotshop` deployed
  WHEN the operator runs `gcx instrumentation clusters apps enable c1 grotshop`
  THEN the backend MUST produce a Beyla pipeline for that namespace
  AND `target_info{}` metric series for that namespace MUST appear in the metrics endpoint within 120 seconds.

### Help-tree shape

- **AC-009**:
  GIVEN the CLI is built
  WHEN the operator runs `gcx instrumentation --help`
  THEN the help output MUST list exactly the subcommands `clusters`, `services`, `setup`, `status` (plus the standard `help` subcommand) and no other top-level subcommands under `instrumentation`.

### Disable + state-machine decay

- **AC-010**:
  GIVEN an instrumented cluster `c1` in state `INSTRUMENTED`
  WHEN the operator runs `gcx instrumentation clusters disable c1 --yes` followed by `gcx instrumentation clusters wait c1`
  THEN the cluster MUST transition through `PENDING_UNINSTRUMENTATION` → `NOT_INSTRUMENTED`
  AND `gcx instrumentation clusters list` MUST eventually omit `c1` after the collector stops reporting
  AND `gcx instrumentation clusters wait c1` MUST exit 0 once the status leaves `PENDING_*`.

### State machine end-to-end

- **AC-011**:
  GIVEN a freshly enabled cluster `c1`
  WHEN the operator runs `gcx instrumentation clusters wait c1 --timeout=10m`
  THEN the command MUST exit 0 once the cluster transitions to `INSTRUMENTED`
  AND MUST exit non-zero if the timeout elapses while the cluster is still `PENDING_INSTRUMENTATION`.

- **AC-012**:
  WHEN the system observes a cluster in terminal `INSTRUMENTATION_ERROR` state
  THEN `gcx instrumentation clusters wait c1` MUST exit non-zero immediately rather than polling to timeout.

### Setup idempotence

- **AC-013**:
  GIVEN a cluster `c1` already configured with default flags and Beyla survey pipeline already created
  WHEN the operator runs `gcx instrumentation setup c1 --yes` twice in succession
  THEN the second invocation MUST report no mutations on stderr
  AND the cluster spec retrieved via `gcx instrumentation clusters get c1` MUST be identical before and after the second invocation.

### Tri-state flag preservation

- **AC-014**:
  GIVEN a cluster `c1` with `costmetrics=true` and `nodelogs=true`
  WHEN the operator runs `gcx instrumentation clusters enable c1 --no-costmetrics`
  THEN `costmetrics` MUST become false
  AND `nodelogs` MUST remain true (preserved because not specified).

- **AC-015**:
  GIVEN a cluster `c1` with namespace `grotshop` and namespace `checkout` both enabled, where `checkout` already has a per-workload override entry (`apps: [{name: payment-svc, selection: SELECTION_INCLUDED}]`)
  WHEN the operator runs `gcx instrumentation clusters apps enable c1 grotshop --tracing`
  THEN the `grotshop` namespace entry MUST be updated (tracing flag set, autoinstrument set true per FR-029)
  AND the `checkout` namespace entry — including its `apps[]` overrides — MUST be byte-equal before and after the operation.

### DWIM service overrides

- **AC-016**:
  GIVEN a namespace `grotshop` with namespace-default autoinstrument and no per-workload overrides
  WHEN the operator runs `gcx instrumentation services include c1 grotshop frontend` twice in succession
  THEN both invocations MUST exit 0
  AND the second invocation MUST be a no-op against the backend representation.

- **AC-017**:
  GIVEN a namespace `grotshop` with a per-workload override on `frontend`
  WHEN the operator runs `gcx instrumentation services clear c1 grotshop frontend`
  THEN the per-workload override MUST be removed
  AND `frontend` MUST inherit the namespace default.

### Output contracts

- **AC-018**:
  WHEN the operator runs `gcx instrumentation clusters list` against a workspace with multiple clusters
  THEN table output MUST present columns ordered with `NAME` first, `STATUS` second-to-last, `AGE` last
  AND the STATUS column MUST contain only `OK`, `FAILING`, or `NODATA` values.

- **AC-019**:
  WHEN the operator runs `gcx instrumentation clusters get c1 -o json` or `gcx instrumentation clusters get c1 -o yaml`
  THEN the output MUST NOT contain a `kind:` field at the top level
  AND MUST NOT contain an `apiVersion:` field at the top level
  AND MUST contain the underlying proto enum value for instrumentation status.

- **AC-020**:
  WHEN the operator runs `gcx instrumentation services list --status=ERROR`
  THEN the output MUST contain only services in terminal error state.

### Pre-Alloy pending-cluster enumeration

- **AC-021**:
  GIVEN a cluster `c1` enabled via `gcx instrumentation clusters enable c1` with no Alloy collector yet running (no `survey_info{}` series for `c1`)
  WHEN the operator runs `gcx instrumentation clusters list -o yaml` (or `-o json`, or `-o wide`) within 60 seconds of the enable call
  THEN `c1` MUST appear in the list output with status `PENDING_INSTRUMENTATION` (in JSON/YAML/wide; default text output normalizes to `NODATA` per FR-051). The cluster is sourced from `PipelineService.ListPipelines()` filtered by K8s monitoring pipeline metadata (per FR-018) and is absent from the `RunK8sMonitoring()` response.

- **AC-021a**:
  GIVEN the same pre-Alloy cluster `c1`
  WHEN the operator runs `gcx instrumentation clusters get c1 -o yaml` (or `-o json`, or `-o wide`)
  THEN the command MUST exit 0
  AND the output MUST report status `PENDING_INSTRUMENTATION` (in JSON/YAML/wide; per FR-019 fallback path).

### Cross-cutting agent-mode behavior

- **AC-022**:
  WHEN the redesigned `gcx instrumentation` commands run with `GCX_AGENT_MODE=true`
  THEN the cross-cutting agent-mode behaviors MUST apply uniformly across `setup`, `status`, `clusters*`, `services*`:
  - **F-AGENT-01**: empty list outputs MUST return `[]`, never `null`. Verified on `clusters list` (when no clusters configured), `services list` (when no workloads observed), and `clusters apps list <empty-cluster>` (when the cluster has no namespace entries).
  - **F-AGENT-02**: mutating-command errors MUST surface root-cause information in an `error.details` JSON field (rather than only a free-form message string).
  - **F-AGENT-03**: confirmation prompts under agent mode MUST either auto-accept (when the calling agent has signalled non-interactive operation) OR fail-fast with a structured `REQUIRES_FORCE` exit code rather than blocking on stdin.
  - **F-AGENT-04**: imperative commands MUST NOT mutate any source files on disk. (Vacuously satisfied for this surface — none of the new verbs accept `-f` inputs.)

## Negative Constraints

- **NC-01**: **NEVER register any GVK** for the `instrumentation` provider. `Registrations()` MUST return an empty slice.
- **NC-02**: **NEVER expose instrumentation kinds via `gcx resources push/pull/get/delete`.** `gcx resources schemas` MUST NOT list any `instrumentation.grafana.app` kind, and `gcx resources push` MUST reject `instrumentation.grafana.app/v1alpha1` documents with `unknown kind`.
- **NC-03**: **DO NOT ship `observability/instrumentation/cluster.yaml` or `observability/instrumentation/apps.yaml`** schema files. They MUST be absent from the repository.
- **NC-04**: **NEVER enforce `metadata.name`** on the `App` type. The `validateAppIdentity` function MUST NOT exist; App identity is positional `(cluster, namespace)`.
- **NC-05**: **NEVER synthesize a client-side tombstone** to make `disable` appear immediate. The cluster naturally drops out of `list` once the collector stops reporting; this decay window MUST be documented in `disable --help`, not hidden.
- **NC-06**: **NEVER execute `helm install` from the `setup` command.** `setup` MUST only print a parameterized helm command. Auto-execution is tracked in #546 and out of scope here.
- **NC-07**: **NEVER wrap JSON or YAML output in a K8s envelope.** Output MUST be the bare domain type with no `kind:`/`apiVersion:` fields.
- **NC-08**: **DO NOT collapse the declared and observed read paths into a single verb.** `get`/`list` MUST treat declared-state endpoints (`GetK8SInstrumentation`, `GetAppInstrumentation`, `PipelineService.ListPipelines` for enumeration fallback) as the source of truth for resource existence and configuration; observed-state endpoints (`RunK8sMonitoring`) MAY be cross-referenced for STATUS only. `status`/`wait` MUST go through observed-state endpoints (with the merge from FR-018/019/024/046 to surface pre-Alloy clusters as `PENDING_INSTRUMENTATION`). The no-collapse rule preserves the invariants that motivated the split: declared writes never lag through an observed read in `get`/`list`, and `list` enumeration never disagrees with `get` lookup on cluster existence.
- **NC-09**: **NEVER expose a user-facing `Selection` flag on `App`** (i.e., on the namespace-level `App` type). The field is dead in the backend (`pkg/instrumentation/v1/k8s_beyla.go:291`). The namespace on/off control surface is the verb pair `clusters apps enable` (always sets `Autoinstrument=true` per FR-029) and `clusters apps disable` (removes the namespace from `namespaces[]`); per-workload `INCLUDED`/`EXCLUDED` overrides are reached only via `services include`/`exclude`/`clear`. Per-app `Selection` (inside `AppNamespace.apps[]`) IS used by the backend and IS the user-facing knob — but ONLY through the `services` verbs, never as a flag on `apps enable`.
- **NC-10**: **NEVER expose a user-facing `Selection` flag on `Cluster`.** The field is retained internally for serialization correctness but MUST NOT be set via flags; user-facing controls are `enable`/`disable`/`reset` verbs.
- **NC-11**: **DO NOT include `Cluster.Selection` in table or wide output.** It is redundant with STATUS and would confuse the human-facing view.
- **NC-12**: **NEVER perform read-modify-write without a client-side optimistic-lock check.** The backend has no server-side optimistic locking (`pkg/storage/primary/mysql/pipeline_storage.go:219-255`); the client comparison is the only conflict signal.
- **NC-13**: **DO NOT auto-detect cluster name from KUBECONFIG.** Cluster name is positional. Auto-detection is deferred future work.
- **NC-14**: **DO NOT introduce a hybrid surface** that exposes both the action verbs and any CRD kinds (`-f manifest.yaml`, `gcx resources push` for instrumentation, etc.) simultaneously. The hybrid was explicitly rejected in ADR-016.

## Risks

| Risk | Impact | Mitigation |
|---|---|---|
| GitOps users (audience B) have no migration path today | Stalls platform-engineer adoption | Documented as out-of-scope future work; backend evolution required (true CRUD endpoints, resource versions, server-side tombstones); revisit triggers a single `instrumentation apply -f` addition rather than coexistence |
| Two read paths to learn (declared vs observed) | Onboarding cost; risk of misuse | `get`/`list` and `status`/`wait` have distinct help text explaining the split; D9 contract documents that `status` is always observed; bare-type JSON exposes the proto enum to remove ambiguity |
| `disable` UX has a 5-minute decay window | Disabled cluster stays visible in `list`/`status` until collector stops reporting | Documented in `disable --help`; `wait` provides programmatic synchronization; not hidden behind a synthetic tombstone |
| D9 Contract 2 (K8s envelope) divergence | Bare-type JSON may surprise users expecting envelope | Documented in ADR-016 and `--help`; justified by no-GVK-registration design; consistent across all instrumentation commands |
| Pre-Alloy enumeration gap (resolved) | `RunK8sMonitoring` does NOT surface clusters whose Alloy hasn't started reporting `survey_info` (gating verified in `pkg/discovery/v1/prometheus.go:204-306`); naive single-source `clusters list` would render audience-A's just-enabled cluster invisible | FR-018/FR-019 require merge with `PipelineService.ListPipelines` filtered by K8s monitoring pipeline metadata; AC-021/AC-021a verify both paths; the merge cost is one extra RPC per `list` invocation |
| Client-side optimistic lock is best-effort | Backend is last-writer-wins; race between read and re-read can still produce silent overwrite | The check narrows the window from "always lost" to "only at the read+write boundary"; users running automated workflows MUST serialize their writes; documented in ADR-016 §Negative |
| `setup` mutation summary may be too verbose under `--yes` automation | Stderr noise in CI logs | Loud mutation summary is intentional (the audience-A onboarding contract requires visible mutation); CI users can redirect stderr; structured (machine-parseable) format is a candidate follow-up |
| Removing `metadata.name` enforcement on App may break callers depending on positional-vs-named identity | Unlikely (App identity is intrinsically positional in the backend) | Zero in-tree callers depend on `metadata.name`; covered by FR-016/FR-017 + AC for app commands |

## Open Questions

All open questions are resolved. Items previously tagged `[DEFERRED]` (GitOps, embedded helm install, KUBECONFIG auto-detect, per-workload override flags, smoke-loop revalidation) are decisions, not gaps — they live in `## Out of Scope` (or, for smoke-loop revalidation, are an obvious post-implementation activity that does not need spec-level tracking).

- **[RESOLVED] Open Question 1 — pre-Alloy pending-cluster enumeration.** Empirically resolved against `grafana/fleet-management` source. `RunK8sMonitoring` builds its cluster set from `extractSurveyInfoClusters` (`pkg/discovery/v1/prometheus.go:204-306`), which queries `survey_info{}` and only enters clusters that already have that metric series. Clusters that have been `Set` via `SetK8SInstrumentation` but whose Alloy collector has not started reporting `survey_info` are NOT surfaced by this RPC. The state-machine join at `pkg/discovery/v1/http.go:264-268` only operates on clusters already in the iteration, so the existing reading "PENDING_INSTRUMENTATION is emitted for set-but-not-observed clusters" was correct only for the subset of clusters already reporting `survey_info` but not yet `kube_node_info`. The fully pre-Alloy window requires the `PipelineService.ListPipelines` fallback. FR-018, FR-019 codify the merge; AC-021, AC-021a verify both paths.
- **[RESOLVED] Audience selection** — A primary, C secondary, B deferred. Locked at brainstorming time and confirmed in ADR-016 §Context.
- **[RESOLVED] CRD facade vs action verbs** — action verbs win. ADR-016 supersedes ADR-014 and rejects Design A (declarative CRUD), tactical CRUD-with-targeted-fixes, and the hybrid alternative.
- **[RESOLVED] `services` placement** — top-level under `gcx instrumentation`, not nested under `clusters`. Justified by `RunK8sDiscovery` being a single fleet-wide RPC.
- **[RESOLVED] Output envelope** — bare domain type. Explicit divergence from D9 Contract 2, justified by no-GVK-registration.
