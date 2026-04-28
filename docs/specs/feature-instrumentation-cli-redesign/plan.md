---
type: feature-plan
title: "gcx instrumentation CLI redesign: action verbs over Set/Get + observed state"
status: approved
spec: spec.md
created: 2026-04-27
---

# Architecture and Design Decisions

## Pipeline Architecture

### Where the feature sits in gcx

```
                        gcx CLI (cobra root)
                                 |
        +------------------------+------------------------+
        |                        |                        |
        v                        v                        v
  cmd/gcx/setup/         cmd/gcx/instrumentation/   cmd/gcx/resources/
  command.go             command.go (NEW)           (UNCHANGED — instrumentation
  (login wizard etc.)            |                  kinds NOT registered)
                                 |
                          subcommand groups
       +--------+---------+-----------+----------+
       |        |         |           |          |
       v        v         v           v          v
   setup     status    clusters    services   (no apps top-level —
   (D2       (--cluster (list/      (list/     apps lives under
   wizard)   --namespace get/       get/        clusters/)
             observed)  enable/     include/
                        disable/    exclude/
                        reset/      clear)
                        wait/
                        apps)
                          |
                          v
                    clusters/apps
                    (list/get/enable/
                     disable/reset/wait
                     — namespace-scoped
                     RMW on App blob)

        |
        | each command Run() builds:
        v
  internal/providers/instrumentation/Provider
       |  (types.go rewritten — no metadata.ObjectMeta;
       |   App.Autoinstrument added; App.Selection dropped;
       |   Cluster.Selection retained internal-only)
       |
       |  Provider.Registrations() → []  (empty; no GVK)
       v
  internal/fleet/Client (shared HTTP base — unchanged)
       |
       | gRPC-Web / Connect-RPC
       v
  fleet-management API
   - InstrumentationService { GetK8s/SetK8s,
                              GetApp/SetApp }
   - DiscoveryService       { SetupK8sDiscovery,
                              RunK8sMonitoring,
                              RunK8sDiscovery }
```

### Read-path split (declared vs observed) — FR-018..025

The design splits at the verb level into two read planes, with bounded cross-reference. `clusters list` and `clusters get` return *what was declared* — using `GetK8SInstrumentation` for configuration, `PipelineService.ListPipelines` as the enumeration fallback for clusters that are `Set` but not yet reporting (D-14, FR-018, FR-019), and `RunK8sMonitoring` as a STATUS cross-reference only. `status` and `wait` return *what the collector is reporting*, layered on the same merge so the cluster set agrees with `list`/`get`.

```
                     Read intent
                          |
            +-------------+--------------+
            |                            |
            v                            v
       "what was         "what is the system
       declared?"         actually doing?"
            |                            |
   declared-state path           observed-state path
            |                            |
            v                            v
   GetK8sInstrumentation        RunK8sMonitoring
   (per cluster)                (fleet-wide list with
                                 state machine per cluster
                                 + per namespace)

   GetAppInstrumentation        RunK8sDiscovery
   (per cluster, single         (fleet-wide list with
    RPC, returns full           state machine per workload)
    namespaces[])

            |                            |
            v                            v
   clusters list                  status [--cluster X]
   (RunK8sMonitoring ⋃           clusters wait <c>
    ListPipelines merge —        clusters apps wait <c> <ns>
    D-14, FR-018; then            services list [--status STATE]
    parallel GetK8s capped       services get
    at 10)
   clusters get <c>
   (GetK8sInstrumentation +
    RunK8sMonitoring; falls back
    to ListPipelines when cluster
    absent from observed —
    FR-019, AC-021a)
   clusters apps list <c>
   clusters apps get <c> <ns>
            |                            |
            +-------------+--------------+
                          v
                output codec dispatch
                (json/yaml/text/wide)
                — all paths emit
                  bare-domain types
                  (no K8s envelope —
                   FR-053, NC-07)
```

**Why split at the verb level (D-08 below).** A single read verb that mixed the two planes would race a declared-state write against an observed-state lookup (`apps get` returning not-found right after a successful write, because the observed query lags the declarative store), and would surface inconsistency between `list` enumeration and `get` lookup (two API paths, two answers). Splitting at the verb level makes the contract impossible to violate at the surface: `get`/`list` are sourced from declared-state endpoints (with `RunK8sMonitoring` consulted only for STATUS cross-reference), and `status`/`wait` are sourced from observed-state endpoints (with pre-Alloy clusters merged in from `ListPipelines` per D-14 so the cluster set stays consistent with `list`).

### Write path — RMW with client-side optimistic-lock (FR-026..037)

Backend has no server-side optimistic locking (`pkg/storage/primary/mysql/pipeline_storage.go:219-255` — last-writer-wins on unversioned MySQL). The provider implements client-side guard:

```
   clusters enable / clusters disable / clusters reset
   clusters apps enable / disable / reset
   services include / exclude / clear
                          |
                          v
              +----------------------+
              | rmw.Update(ctx,      |
              |   getFn,             |
              |   mutateFn,          |
              |   setFn,             |
              |   compareFn,         |
              |   maxRetries=3)      |
              +----------------------+
                          |
            +-------------+-------------+
            v                           v
    1. GET current               2. apply mutation
       (fresh fetch on              (tri-state flag
        every retry — no            preservation; never
        stale snapshot              touch unrelated
        reused after                namespaces)
        conflict)
                                         |
                                         v
                    3. GET again immediately before write
                                         |
                                         v
                       compare(snapshotAtRead,
                               currentBeforeWrite)
                                         |
                            +------------+------------+
                            |                         |
                       same as read              changed under us
                            |                         |
                            v                         v
                   4a. SET (write)            4b. fail-fast with
                                                   error naming
                                                   the conflicting
                                                   namespace and
                                                   the diff (FR-030)
```

The compare implementation reuses the field-by-field equality approach from the legacy `internal/setup/instrumentation/compare.go`, generalized over the new `App` and `Cluster` types in `internal/providers/instrumentation/types.go`.

### State machine (D-08, D-11, D-12)

`wait` and `status` consume the proto `instrumentation_status` enum returned by `RunK8sMonitoring` / `RunK8sDiscovery`. The transitions and triggers are reactive (Prometheus metric presence/absence at query time), not time-based:

```
                       SetK8S/SetApp(INCLUDED)
NOT_INSTRUMENTED ──────────────────────────────► PENDING_INSTRUMENTATION
       ▲                                                     │
       │                                                     │ collector reports
       │ collector stops reporting                           │ (kube_node_info /
       │                                                     │  target_info)
       │                                                     ▼
PENDING_UNINSTRUMENTATION ◄────────────────────── INSTRUMENTED
                          SetK8S/SetApp(EXCLUDED
                          or removed entry)

EXCLUDED — set by user via Selection=SELECTION_EXCLUDED;
           kept visible by RunK8sMonitoring (no PENDING_EXCLUSION
           transition — backend has no notion).
ERROR    — defined in proto; never set by current backend code
           (reserved for future use, but FR-049 / AC-012 require
            wait to fail on it if it ever surfaces).
```

There is no server-side stale-data timeout — every read of `RunK8sMonitoring`/`RunK8sDiscovery` re-queries Prometheus. The "5-minute decay window" on `disable` is a consequence of collectors polling `GetConfig` at their own cadence; the cluster drops out of `list` only after the collector observes the pipeline is gone and stops reporting `kube_node_info{}`.

### Setup wizard data flow (FR-038..045)

```
   gcx instrumentation setup <cluster> [--yes] [--print-helm-only]
                          |
   +----------------------+-----------------------+
   |                                              |
   --print-helm-only                       (default: mutating)
   |                                              |
   v                                              v
   formatHelmCommand(stack-derived urls)    SetupK8sDiscovery
   (no server calls)                        (idempotent — no-op
   |                                         if Beyla survey
   |                                         pipeline exists)
   |                                              |
   |                                              v
   |                                        GetK8sInstrumentation
   |                                              |
   |                                              v
   |                                        promptForFlags()  if TTY && !--yes
   |                                        applyDefaults()    if --yes
   |                                              |
   |                                              v
   |                                        SetK8sInstrumentation
   |                                        (only if any flag changed)
   |                                              |
   |                                              v
   |                                        emitMutationSummary(stderr)
   |                                              |
   |<------------------------+--------------------+
                             v
                  printHelmCommand(stdout)
                  parameterized with stack URL,
                  username, access-policy token
```

Re-running on a fully-configured cluster prints "no changes" mutation summary (FR-045 / AC-013).

## Design Decisions

| # | Decision | Rationale | Spec ref |
|---|----------|-----------|----------|
| D-01 | Mount the new tree at `cmd/gcx/instrumentation/command.go` and wire it from `cmd/gcx/root/command.go` as a peer of `setup/`, `resources/`, `datasources/`, etc. | Action-verb tree is a top-level concern. Nesting under `setup/` would conflate cluster onboarding (a verb) with cluster configuration (declarative state) and force the wizard to live as a sibling of unrelated setup flows. | FR-001, FR-002, FR-003, FR-004 |
| D-02 | The onboarding wizard is mounted as the top-level `gcx instrumentation setup`, NOT as a subcommand under `clusters`. | The wizard is conceptually onboarding (one-shot, mutating, prints helm), not a sub-resource of clusters. Top-level placement makes audience-A's "instrument this cluster end-to-end" path a single command. | FR-001, FR-038 |
| D-03 | The provider package `internal/providers/instrumentation/` is rewritten in place. `provider.go` continues to register under `internal/providers/` (so `gcx providers` lists it), but `Registrations()` returns an empty slice. | Provider self-registration is how `gcx providers` discovers the surface; we keep the listing intact while explicitly declining adapter-pipeline integration. | FR-006, FR-007, FR-008, FR-009, FR-010, NC-01, NC-02 |
| D-04 | The repository MUST NOT contain `observability/instrumentation/cluster.yaml` or `observability/instrumentation/apps.yaml`; if any predecessor branch left them present, the implementation deletes them and removes their imports from any resources-discovery registry that references them. | The redesign explicitly forbids `gcx resources push/pull` from carrying instrumentation kinds; schema files would be inert and misleading. | FR-009, NC-02, NC-03 |
| D-05 | Internal types live in `internal/providers/instrumentation/types.go` as bare-domain Go structs (`App`, `Cluster`, plus observed-status companions). No `metav1.ObjectMeta`, no `TypeMeta`, no GVK, no `kind/apiVersion` fields, no `metadata.name` requirement. App identity is `(cluster, namespace)` positional. | The backend is `Set/Get` over a per-cluster blob; forcing K8s envelope semantics onto it loses information (the "unset" state collapses to a zero value) and creates round-trip parity expectations the backend cannot honor. Types must mirror the proto, not the K8s ResourceModel. | FR-011, FR-012, FR-013, FR-014, FR-015, FR-016, FR-017, NC-04, NC-09 |
| D-06 | Tri-state CLI flags (`--<flag>` / `--no-<flag>` / unset) are modelled internally as `*bool` so "unset" is preserved on RMW. The codec pipeline preserves `nil → omit` on serialize and `nil ≠ false` on compare. | The K8s-envelope route would coerce unset to false on round-trip, producing silent overrides on PUT under partial mutation. Pointer types preserve the three states explicitly. | FR-027, AC-014, AC-015 |
| D-07 | Add a small `internal/providers/instrumentation/rmw/` helper exposing `Update[T any](ctx, getFn, mutateFn, setFn, equalsFn, maxRetries)` returning a structured `ConflictError` on exhaustion. The compare implementation generalizes the snapshot-equality approach already in `internal/setup/instrumentation/compare.go`. | Backend is last-writer-wins on unversioned MySQL; without a client guard, concurrent `enable` calls silently clobber. The RMW helper centralizes the read-modify-compare-write contract so no command site can accidentally skip it (NC-12). | FR-026, FR-028, FR-030, AC-006 |
| D-08 | Read paths split into two distinct verb sets: declared-state (`clusters get`, `clusters list`, `clusters apps get`, `clusters apps list`) versus observed-state (`status`, `clusters wait`, `clusters apps wait`, `services list`, `services get`). | A collapsed read path would race declared writes against observed reads (apps get returning not-found right after a successful write) and would surface inconsistency between list enumeration and get lookup. Splitting at the command-tree level makes the contract impossible to violate. | FR-018..025, NC-08 |
| D-09 | A per-provider output codec adapter at `internal/providers/instrumentation/output/` registers bare-domain marshallers with `internal/output/`. Default formats: `text` (table), `wide` (table + extra cols), `json`, `yaml`. STATUS column is normalized to `OK/FAILING/NODATA`; the underlying proto enum is exposed in `wide`/`json`/`yaml`. | The shared codec registry assumes K8s `unstructured.Unstructured` for default field discovery; bare-domain types must opt into the registry explicitly. STATUS normalization gives cross-provider parity in human output while preserving fidelity in machine output. | FR-050, FR-051, FR-052, FR-053, FR-054, AC-018, AC-019, NC-07 |
| D-10 | The setup wizard emits a runnable `helm` command via a dedicated formatter package `internal/providers/instrumentation/helm/` (string builder + escaping + flag ordering). gcx never invokes `helm` itself. | The spec forbids gcx from executing `helm install` (NC-06); treating the helm command as data (formatted output) keeps the safety boundary visible and testable. | FR-042, FR-044, NC-06 |
| D-11 | The `wait` commands use a poller in `internal/providers/instrumentation/wait/` that calls `RunK8sMonitoring` (clusters) or `RunK8sDiscovery` / filtered `RunK8sMonitoring` (apps) on a fixed 5-second cadence with a default 5-minute timeout. The poller exits 0 on transition out of `PENDING_*`, non-zero on timeout, non-zero on terminal `INSTRUMENTATION_ERROR`. | `wait` is a UX affordance over the observed-state read; isolating the polling logic lets it be unit-tested with a fake monitoring client and lets future server-push (streaming) replace it without touching command code. The 5s cadence matches the collector-app UI's polling interval. | FR-046, FR-047, FR-048, FR-049, AC-010, AC-011, AC-012 |
| D-12 | `clusters disable`, `clusters reset`, `clusters apps disable`, `clusters apps reset` are gated on `--yes` (no interactive prompt for now). | Destructive verbs that delete server-side pipelines or wipe user customization need explicit consent. The non-interactive form supports CI use; an interactive prompt is deferred future work. | FR-031, FR-032, FR-033, FR-034 |
| D-13 | `services include`/`exclude`/`clear` operate on a single `(cluster, namespace, service)` tuple via `GetAppInstrumentation` → mutate the targeted entry inside the namespace's `apps[]` → `SetAppInstrumentation` under the RMW helper. All three are idempotent. | The whole-list-replace primitive (`SetAppInstrumentation`) makes naive partial mutations dangerous; encoding DWIM into the verbs (rather than user-supplied payloads) makes the contract correct by construction. | FR-035, FR-036, FR-037, AC-016, AC-017 |
| D-14 | Pre-Alloy pending-cluster enumeration is resolved (Open Question 1, RESOLVED in spec.md). The fleet-management source (`pkg/discovery/v1/prometheus.go:204-306`) confirms `RunK8sMonitoring`'s cluster set is gated by `survey_info{}`; clusters that are `Set` but not yet reporting are invisible to that RPC. Implementation: `clusters list` (FR-018), `clusters get` (FR-019), `status` (FR-024), and `clusters wait` (FR-046, pre-flight pipeline existence check) MUST merge `RunK8sMonitoring()` results with `PipelineService.ListPipelines()` filtered by K8s monitoring pipeline metadata. The merge is implemented in a small `internal/providers/instrumentation/enumerate/` helper that takes both client interfaces, returns a unified `[]K8SCluster` slice keyed by name, and assigns `PENDING_INSTRUMENTATION` to any cluster present only in pipeline state. The helper is unit-testable with fakes for both clients and reused by all four call sites. | Naive single-source enumeration would render audience-A's just-enabled cluster invisible until Alloy starts, breaking AC-007 / AC-021. The merge cost is one extra RPC per `list`/`get`/`status` call (and one pre-flight RPC for `wait`), which is acceptable. | FR-018, FR-019, FR-024, FR-046, AC-007, AC-021, AC-021a |
| D-15 | Cobra wiring is performed in `wire/wire.go` (or equivalent bootstrap). The wire registers the `instrumentation` command tree but explicitly does NOT call any `internal/resources/adapter.RegisterAdapter` for instrumentation kinds. `internal/providers/instrumentation/Provider.Registrations()` returns `nil`. | The provider must appear in `gcx providers` (so users discover it) without appearing in `gcx resources schemas` (FR-007) or being walked by `gcx resources push/pull` (FR-008). | FR-006, FR-007, FR-008, FR-010, NC-02 |
| D-16 | All commands are agent-mode-aware via `internal/agent` annotations and emit structured JSON when `GCX_AGENT_MODE=true`, including state-machine values, optimistic-lock conflict errors, and the helm command from `setup`. | Cross-cutting agent-mode contract (F-AGENT-01..04) — ensures the redesign matches the rest of gcx's agent-first surface from day one. Inherits whatever the cross-cutting fix lands as. | AC-022 |
| D-17 | KUBECONFIG is never auto-detected. The wizard takes the cluster name as a positional arg; auto-detection is deferred future work (Out of Scope). | Auto-detection would couple gcx to local Kubernetes state, breaking CI use and producing surprise targets. | NC-13 |

## Compatibility

### What this surface relies on (unchanged dependencies)

- `internal/fleet/` shared HTTP base client — no changes; the new instrumentation provider layers on top with the existing auth + retry transports.
- `gcx providers` — the instrumentation provider appears in the listing with `Registrations()` returning empty, signalling "command-tree-only" surface (FR-006).
- All other providers' `ResourceAdapter` registrations and the `internal/resources/adapter.Factory` route table are untouched. `gcx resources push/pull/get/schemas/delete/edit/validate` continue to behave identically for every other provider (dashboards, folders, alert rules, faro apps, fleet pipelines, IRM, SLO, synthetic monitoring, k6, etc.).
- Top-level `gcx setup` remains the umbrella for non-instrumentation onboarding flows (login wizard, etc.); instrumentation onboarding lives at the dedicated top-level `gcx instrumentation setup` per D-02.

### What this surface explicitly does NOT support

- No declarative manifest workflow. `gcx resources push -p` paths containing `apiVersion: instrumentation.grafana.app/v1alpha1` documents (or any `Cluster`/`App` kind under that group) MUST be rejected with `unknown kind` (FR-008, AC-002). `gcx resources pull` MUST return nothing for instrumentation (FR-007, AC-001).
- No `observability/instrumentation/cluster.yaml` or `observability/instrumentation/apps.yaml` schema files (FR-009, D-04).
- No `-f manifest.yaml` flag on any of the new verbs. App identity is positional `(cluster, namespace)`; cluster identity is positional `<cluster>`; `metadata.name` is never read or required (FR-016, FR-017, NC-04).
- No GitOps `apply -f instrumentation.yaml` flow. Re-entry deferred to backend evolution (Out of Scope §1).

### What is newly available

- `gcx instrumentation setup <cluster>` — top-level guided onboarding wizard (TTY + `--yes`) that emits a runnable `helm` command, with idempotent re-run and loud mutation summary (FR-038..045, AC-013).
- `gcx instrumentation status [--cluster] [--namespace]` — observed-state view across the hierarchy, drilling via flags rather than verb nesting; D9 Contract 3 STATUS normalization (FR-024, FR-025, AC-007).
- `gcx instrumentation clusters wait <cluster>` and `gcx instrumentation clusters apps wait <cluster> <namespace>` — poll-until-state-machine-leaves-PENDING with `--timeout` (FR-046..049, AC-010..012).
- `gcx instrumentation clusters enable <cluster>` and `gcx instrumentation clusters apps enable <cluster> <namespace>` — tri-state RMW with optimistic-lock guard (FR-026..030, AC-006, AC-014, AC-015).
- `gcx instrumentation clusters disable <cluster>` and `gcx instrumentation clusters apps disable <cluster> <namespace>` — `--yes`-gated destructive disable; cluster naturally drops from `list` once collector stops reporting (FR-031, FR-032, AC-010, NC-05).
- `gcx instrumentation clusters reset <cluster>` and `gcx instrumentation clusters apps reset <cluster> <namespace>` — `--yes`-gated restore-to-defaults distinct from disable (FR-033, FR-034).
- `gcx instrumentation services list [--cluster] [--namespace] [--status STATE] [--all]` — fleet-wide observed-state list with audience-C "find broken services" path via `--status=ERROR` (FR-022, FR-054, AC-020).
- `gcx instrumentation services get <cluster> <namespace> <service>` — single-workload observed-state read (FR-023).
- `gcx instrumentation services include`/`exclude`/`clear` — DWIM per-workload override on a single `(cluster, namespace, service)` tuple, idempotent, RMW with optimistic-lock guard (FR-035..037, AC-016, AC-017).
- Optimistic-lock conflict surfacing on every RMW path with a structured error message naming the conflicting namespace and the detected change (FR-030, AC-006).
- Bare-domain JSON/YAML output: every `--output json|yaml` returns the proto-shaped struct directly, with no `kind`/`apiVersion`/`metadata` envelope; the proto enum value for instrumentation status is exposed in `json`/`yaml`/`wide` (FR-052, FR-053, AC-019, NC-07).
- Cross-provider STATUS normalization (`OK`/`FAILING`/`NODATA`) in human-facing table output, with the underlying state-machine value preserved in machine output (FR-051, AC-018).
