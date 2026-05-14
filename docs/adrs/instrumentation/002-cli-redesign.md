# `gcx instrumentation` CLI redesign: action verbs over Set/Get + observed state

**Created**: 2026-04-25
**Last updated**: 2026-04-28
**Status**: accepted
**Supersedes**: [docs/adrs/instrumentation/001-instrumentation-provider-design.md](001-instrumentation-provider-design.md)
**Implementation**: PR [#597](https://github.com/grafana/gcx/pull/597)

<!-- Status lifecycle: proposed -> accepted -> deprecated | superseded -->

## Context

Grafana Cloud's Instrumentation Hub is backed by two fleet-management
services: `instrumentation.v1.InstrumentationService` (per-cluster `Set/Get`
on a configuration blob) and `discovery.v1.DiscoveryService` (collector-
observed monitoring state with a built-in
`PENDING_INSTRUMENTATION → INSTRUMENTED → PENDING_UNINSTRUMENTATION → NOT_INSTRUMENTED`
state machine). The two are joined server-side at read time, but exposed as
RPCs — there is no per-resource CRUD, no `resourceVersion`, no `status`
subresource, no per-cluster delete RPC.

The previous instrumentation work (PR #531, now closed) exposed this
backend as an `instrumentation.grafana.app/v1alpha1` CRD-style facade:
`kind: Cluster`, `kind: App`, push/pull through `gcx resources`. A smoke
loop run against PR-531's build found a coherent set of bugs whose root
cause is the abstraction leak rather than independent defects:

| Finding | Symptom | Root cause |
|---|---|---|
| B-531-02 | `apps get` returns not-found right after `push` succeeded | declared state stored, observed state queried — no overlap window |
| B-531-05 | `list`/`status` return `[]` for a cluster that `get` returns | `list` reads observed state, which lags Alloy registration |
| B-531-08 | `delete` has no effect — Alloy keeps re-registering the cluster | backend has no tombstone; CRD `delete` semantics promise something the API can't deliver |
| B-531-01 | `resources pull` returns 0 for kinds `push` just accepted | round-trip parity is part of the `gcx resources` contract; backend can't honor it |

PR #531's surface produced these symptoms because it spoke a CRUD vocabulary
that the backend does not implement.

The smoke-loop document framed the resolution as a binary: **Design A**
(fully declarative CRUD — push writes desired state, list/get expose
declared+observed via a `status` subfield) or **Design B** (drop the CRUD
costume — action verbs, observed state as primary, no `gcx resources`
integration). Design A requires backend changes (true CRUD endpoints,
resource versions, server-side tombstones) that are not on the immediate
roadmap; Design B fits the backend as it exists today.

ADR #014 (`001-instrumentation-provider-design.md`) staked an earlier
position — instrumentation as a declarative-manifest workflow under a
`gcx setup` framework with optimistic-locking `apply -f`. PR #531
implemented an evolution of that direction (top-level `gcx instrumentation`
with CRD kinds plus push/pull) and the smoke loop demonstrated that the
declarative facade does not survive contact with the Set/Get + observed-
state backend. ADR #014 is superseded by this redesign; the rationale that
remained valid (per-namespace/per-workload granularity, agent-friendliness,
shared `internal/fleet/` client) is preserved here in non-declarative form.

The audiences for this surface are locked from prior brainstorming:

- **Primary — A**, day-1 onboarding operator: "I have a new cluster, I want
  it instrumented end-to-end." Pulls `setup`, action verbs, `wait` for
  state-machine transitions.
- **Secondary — C**, day-N investigator/SRE: "An app isn't producing
  telemetry — show me what Beyla sees." Pulls `status`, `services list
  --status=ERROR`, granular per-workload reads.
- **Deferred — B**, GitOps/platform engineer: listed for completeness; does
  not constrain this design and is blocked on backend evolution.

## Decision

We will replace the PR-531 CRD facade with an action-verb command tree
grounded in the actual fleet-management API shape. The `instrumentation`
provider registers no GVKs and is not addressable through `gcx resources
push/pull/get/delete`.

> **Note on verb names.** This section records the verbs as originally
> proposed (`enable` / `disable` / `reset`). Iteration 2 renamed
> `enable` → `configure`, `disable` → `remove`, and folded `reset` into
> `configure --use-defaults --yes`; iteration 3 standardized boolean
> flags as `--feat=true|false` (no `--no-*` paired variants). Current
> verb names live in the Amendments sections below; the verb semantics
> here remain accurate.

### Command tree

```
gcx instrumentation
├── setup <cluster>                                    # D2 onboarding wizard, mutating, loud
├── status [--cluster X] [--namespace ns]              # observed view across hierarchy
├── clusters
│   ├── list
│   ├── get <cluster>
│   ├── enable <cluster> [flag-mods]                   # RMW; tri-state flags
│   ├── disable <cluster>                              # destructive; --yes-gated
│   ├── reset <cluster>                                # destructive; --yes-gated
│   ├── wait <cluster> [--timeout=5m]
│   └── apps
│       ├── list <cluster>
│       ├── get <cluster> <namespace>
│       ├── enable <cluster> <namespace> [flag-mods]
│       ├── disable <cluster> <namespace>
│       ├── reset <cluster> <namespace>
│       └── wait <cluster> <namespace>
└── services
    ├── list [--cluster X] [--namespace ns] [--status STATE] [--all]
    ├── get <cluster> <namespace> <service>
    ├── include <cluster> <namespace> <service>        # DWIM: ensure instrumented
    ├── exclude <cluster> <namespace> <service>        # DWIM: ensure NOT instrumented
    └── clear <cluster> <namespace> <service>          # remove override → namespace default
```

### Tree placement rationale

`clusters` and `clusters apps` are the **configuration tree** — declared
state lives per-cluster (`Get/SetK8SInstrumentation`,
`Get/SetAppInstrumentation`), and `App` namespace entries are stored as rows
inside the cluster blob. Nesting apps under clusters mirrors that storage
shape.

`services` is the **observation tree** — `RunK8sDiscovery` is a single
fleet-wide RPC; services are not stored entities but a projection over
Prometheus + per-namespace config. Top-level placement honors the API shape
and preserves the audience-C "find broken services across the fleet" path.

`status` is the cross-cutting **observed view**, drilling from cluster →
namespace → service via flags rather than verbs (consistent with the D9
"`status` always reads observed state at any level" contract).

### Read paths split by intent

| Command | API path |
|---|---|
| `clusters list` | merge `RunK8sMonitoring()` ⋃ `PipelineService.ListPipelines()` filtered by K8s monitoring metadata (clusters present only in pipeline state appear with status `PENDING_INSTRUMENTATION`); then fan out `GetK8SInstrumentation` per cluster (parallel, capped at 10). See Update §2026-04-28 below for empirical justification. |
| `clusters get <c>` | `GetK8SInstrumentation(c)` + cross-ref status from a single `RunK8sMonitoring()`; if `<c>` is absent from `RunK8sMonitoring()`, fall back to `PipelineService.ListPipelines()` to determine status (pipeline present → `PENDING_INSTRUMENTATION`; absent → `NOT_INSTRUMENTED`). |
| `clusters apps list <cluster>` | `GetAppInstrumentation(cluster)` (single RPC) |
| `clusters apps get <c> <ns>` | `GetAppInstrumentation(c)` + filter; CLI-level not-found if absent |
| `services list` | `RunK8sDiscovery()` (single RPC, fleet-wide) + client-side filtering |
| `services get <c> <ns> <s>` | `RunK8sDiscovery()` + filter |
| `services include\|exclude\|clear` | `GetAppInstrumentation(c)` → mutate `apps[]` → `SetAppInstrumentation(c, …)` |
| `status` | same cluster set as `clusters list` (merge with `ListPipelines` so pre-Alloy clusters appear); `RunK8sDiscovery()` when drilling to namespace via `--namespace`. |
| `wait` | poll `RunK8sMonitoring()` at 5s intervals; treat absence from response as `PENDING_INSTRUMENTATION` and continue polling; declared-state pre-flight (`GetK8SInstrumentation`) avoids polling-to-timeout when no cluster has been declared (Amendment B). |

`get`/`list` treat declared-state endpoints (`Get*`, `ListPipelines` for
enumeration fallback) as the source of truth for resource existence and
configuration; observed-state endpoints (`RunK8sMonitoring`) are
cross-referenced for STATUS only. `status`/`wait` go through observed-state
endpoints with the same merge so the cluster set agrees with `list`/`get`.
Both read paths exist because both questions are real ("what did I
configure?" vs "what is Beyla actually seeing?"); collapsing them into
one verb is what produced B-531-02 and B-531-05. The original draft of
this ADR specified single-source enumeration on `list`/`get`; Open
Question 1 was resolved empirically (see Update §2026-04-28) and the
merge was introduced before implementation.

### Verb semantics

- **`setup`** — D2-mandated onboarding command. Loud, mutating, idempotent.
  Calls `SetupK8sDiscovery` (server-side idempotent), then prompts for K8s
  flags (or applies defaults under `--yes`), calls `SetK8SInstrumentation`
  if anything changed, prints a parameterized helm command, and emits a
  mutation summary on stderr. `--print-helm-only` is the explicit
  non-mutating opt-in.
- **`enable`** — read-modify-write with tri-state flags
  (`--flag` / `--no-flag` / unset → preserve). For `apps enable` the
  namespace entry is the unit of RMW: existing per-workload `apps[]`
  overrides and other namespaces are preserved. Client-side optimistic-lock
  check fixes the misleading PR-531 error text (B-531-04).
- **`disable`** — destructive. `clusters disable` calls
  `SetK8SInstrumentation(Selection=EXCLUDED)`, which the backend translates
  to pipeline deletion. `apps disable` removes the namespace from
  `namespaces[]`. `--yes`-gated. State machine: cluster transitions
  `INSTRUMENTED → PENDING_UNINSTRUMENTATION → NOT_INSTRUMENTED` and
  naturally drops out of `list` once the collector stops reporting (no
  synthetic tombstone).
- **`reset`** — destructive. Restores defaults, wiping user customization.
  Distinct from `disable`: end state is "instrumented with defaults," not
  "not instrumented."
- **`wait`** — poll the appropriate observed-state endpoint at 5s intervals
  (matching the UI's polling cadence). Exits 0 when STATUS leaves
  `PENDING_*`, non-zero on timeout (default 5m) or terminal
  `INSTRUMENTATION_ERROR`.
- **`services include`/`exclude`/`clear`** — DWIM. Each operates on one
  workload via a single `Get` + targeted `apps[]` mutation +
  `SetAppInstrumentation` round-trip with optimistic-lock guard. All
  idempotent.

### Output contracts

Per Contract 1 (D9): `NAME` first, `STATUS` second-to-last, `AGE` last.
STATUS values follow Contract 3 normalization (`OK` / `FAILING` / `NODATA`)
for cross-provider parity, with the underlying state-machine value
available in JSON/YAML and `wide` format.

JSON/YAML output is the **bare domain type** — no K8s envelope, no `kind:`
or `apiVersion:` field. This is a deliberate divergence from D9 Contract 2,
justified because the resources are not registered with the adapter and not
addressable via `gcx resources`. The Contract 2 envelope exists to support
round-tripping through `gcx resources push/pull`, which we are explicitly
not supporting here.

### Adapter registry change

The `instrumentation` provider's `Registrations()` returns an empty slice —
no GVK is registered, no schema is exposed via `gcx resources schemas`, no
kind is accepted by `gcx resources push`. The `wire/wire.go` bootstrap
still registers the command tree but skips the adapter pipeline. Schema
files in `observability/instrumentation/` (`cluster.yaml`, `apps.yaml`)
are deleted.

### Internal types

`internal/providers/instrumentation/types.go` is rewritten:

- **Drop** `App.Selection` (dead field — `pkg/instrumentation/v1/k8s_beyla.go:291`
  confirms backend ignores `AppNamespace.selection`).
- **Add** `App.Autoinstrument bool` — the actual on/off knob for
  namespace-level instrumentation. Likely root cause of B-531-06: PR-531
  types had no `Autoinstrument` field, so it was never set, so the backend
  produced no Beyla pipeline.
- **`Cluster.Selection`** stays in the type for serialization correctness
  (it is a real backend field) but is not user-controllable. Visible in
  JSON/YAML output for diagnostics; not in table or wide output (redundant
  with STATUS).
- **Add** observed status fields populated from `RunK8sMonitoring` for
  table/wide output, refined to use the proto enum directly rather than
  freeform strings.
- **Remove** `metadata.name` enforcement on App (`validateAppIdentity`).
  App identity is `(cluster, namespace)` positionally on every command.

### Migration

Breaking change. PR #531 was preview-status with no committed users; no
backward-compatibility aliases are provided. The mapping at a glance
(reflecting iteration-2 verb renames captured in Amendment D):
`clusters create -f` and `clusters update -f` collapse into
`clusters configure [flags]`; `clusters delete` becomes `clusters remove`;
`clusters setup` moves to top-level `gcx instrumentation setup`. The
same shape applies for apps under `clusters apps`. `gcx resources push
-p observability/instrumentation` rejects any
`instrumentation.grafana.app/v1alpha1` documents with "unknown kind",
and the schema files under `observability/instrumentation/` are deleted.
A migration note in the breaking-changes section of the PR body and
CHANGELOG explains the move.

### Rejected alternatives

**Design A — Fully declarative CRUD (matches PR-531's API shape).** Push
writes desired state; `list`/`get` return desired state plus a
`status`/`observed` subfield reporting what Alloy sees. "Pushed but not
yet observed" becomes a first-class state. Matches the CRD mental model
and honors the `gcx resources push` uniform-pipeline promise.
**Rejected** because the backend has no `resourceVersion`, no per-resource
delete, no server-side tombstone, and no list-of-configured-clusters RPC.
Implementing Design A means either (a) accepting a thin client-side
simulation of CRUD that lies about its guarantees — which is what PR-531
did, with the documented bug shape — or (b) blocking on a fleet-management
API redesign that is not on the roadmap. Revisit when the backend grows
true CRUD; out-of-scope future work below references this.

**Design B-as-PR-531 — Keep the CRD kinds, fix the symptoms tactically.**
Continue to register `kind: Cluster` and `kind: App`; address each
B-531-* finding with a targeted fix (e.g., make `apps get` read declared
state, make `delete` synthesize a "stop reporting" tombstone, etc.).
**Rejected** because the smoke-loop analysis shows the bugs are
manifestations of one design mismatch; tactical fixes will keep producing
indistinguishable bug reports each time someone new encounters the
abstraction leak. Better stderr hints reduce the frequency, not the shape.

**Hybrid — keep `gcx resources` integration, add imperative commands
alongside.** Surface both paths and let users pick. **Rejected** because
the `gcx resources` integration cannot be made coherent against this
backend (Design A's blockers apply); a half-working second path is worse
than none, and divides command-discovery effort across two surfaces. When
the backend supports it, `instrumentation apply -f` re-enters as a single
addition rather than a coexistence.

**Keep `gcx setup instrumentation` framework from ADR #014.** Earlier
ADR placed instrumentation under a dedicated `setup` area with declarative
manifests and `apply -f`. **Rejected** because (a) the manifest workflow
ran into the same backend blockers as Design A, (b) the `setup` area
generalization to other products did not materialize, and (c) the
audience-A onboarding path is better served by a single top-level
`gcx instrumentation setup <cluster>` wizard than by a multi-product
framework. ADR #014 marked superseded.

## Consequences

### Positive

- **Bug shape fixed at the root.** B-531-02, -05, -08 disappear by
  construction: `apps get` reads declared state with no race, `clusters
  list` reads the same observed-state endpoint as `status`, `disable`
  deletes the pipeline and the cluster transitions through the documented
  state machine instead of "zombie".
- **B-531-06 fix candidate**. New types include `App.Autoinstrument`;
  `enable` always sets it true, exercising the backend's pipeline-create
  path that PR-531 silently bypassed.
- **Audience-A onboarding stays one-command** (`gcx instrumentation setup
  <cluster>`) with idempotent re-run and a loud mutation summary that
  resolves B-531-03's silent-overwrite complaint.
- **Audience-C investigation gets a richer observed view** via
  `status --cluster --namespace` drilldown and `services list --status=ERROR`,
  matching what Beyla actually surfaces.
- **No false promises.** Removing `gcx resources` integration eliminates
  B-531-01 (round-trip parity) and the "uniform pipeline" narrative that
  PR-531's docs leaned on. Schema files are deleted; `gcx resources schemas`
  no longer advertises kinds we cannot honor.
- **Improved error messages.** Optimistic-lock errors name the conflicting
  namespace and the change detected (B-531-04 fix).

### Negative

- **GitOps users have no migration path today.** Users who want
  `apply -f instrumentation.yaml` semantics must wait for backend evolution
  or maintain config out-of-band. Documented as out-of-scope future work.
- **Breaking change.** Any caller built against the PR-531 CRD surface
  must rewrite to the new imperative commands. Mitigated by PR-531 being
  preview-status with no committed users; full migration table provided.
- **Two read paths to learn.** `get`/`list` (declared) and `status`/`wait`
  (observed) are distinct verbs with distinct semantics. The contract is
  documented and consistent, but it is a more nuanced model than "one
  read verb, always the truth." Help text and examples must teach this
  explicitly.
- **`disable` UX has a 5-minute decay window.** A disabled cluster stays
  visible in `list`/`status` (with state machine moving through
  `PENDING_UNINSTRUMENTATION` → `NOT_INSTRUMENTED`) until the collector
  stops reporting. This is honest about backend behavior but surprising
  to users expecting immediate disappearance. Documented in `disable
  --help`.
- **D9 Contract 2 (K8s envelope) divergence.** `instrumentation`
  JSON/YAML output is bare domain types, not envelope-wrapped. Justified
  but it is a documented exception that future readers of the contract
  will need to understand.

### Neutral / Follow-up

- **PR-531 commit `e14256a` salvage.** Plumbing — fleet client extraction,
  table builders, status-enum parsing — may be cherry-picked into the new
  branch in a separate session. Not part of this ADR.
- **Smoke-loop revalidation.** After implementation, the smoke matrix is
  re-run with PASS criteria updated for B-531-02/05/08 to reflect the
  reclassified-as-by-design semantics. Acceptance is tracked outside this
  ADR.
- **Open question 1 — RESOLVED (2026-04-28).** Empirical reading of
  `grafana/fleet-management/pkg/discovery/v1/prometheus.go:204-306`
  showed the original investigation hypothesis was incorrect:
  `RunK8sMonitoring`'s cluster set is built by `extractSurveyInfoClusters`,
  which queries `survey_info{}` (Beyla survey) and only enters clusters
  that already have that metric series. The state-machine join at
  `pkg/discovery/v1/http.go:264-268` only runs over clusters already in
  the iteration. Therefore: clusters that have been `Set` but whose Alloy
  collector has not started reporting `survey_info` are NOT surfaced by
  `RunK8sMonitoring` (they are absent from the response, not present with
  `PENDING_INSTRUMENTATION`). The required mitigation is a merge with
  `PipelineService.ListPipelines` filtered by K8s monitoring pipeline
  metadata. The read-paths table above and the Update §2026-04-28 below
  reflect the resolution.
- **Cross-cutting agent-mode fixes** (empty-list shape, create-error
  details, agent-mode confirmation prompts, source-file mutation) are
  orthogonal to this redesign and addressed via Amendments C and the
  output codec changes here.
- **Out-of-scope future work, contingent on backend evolution.**
  - GitOps integration: when fleet-management exposes `Cluster` and `App`
    as true CRUD with `resourceVersion` and a `status` subresource,
    register them in `gcx resources` and add `instrumentation apply -f`.
  - Embedded helm install / access policy mint: tracked in #546.
  - Auto-detection of cluster context from `KUBECONFIG` for `setup`.
  - Per-workload override flags on `apps configure`: revisit if
    `services include/exclude/clear` proves too verbose.

## Update — 2026-04-28 (Open Question 1 resolution)

This section records the empirical correction to the original ADR draft
that landed before implementation began.

### What changed

1. **Open Question 1 resolved.** The original investigation hypothesis —
   that `RunK8sMonitoring` surfaces `Set`-but-not-Alloy-reporting clusters
   via the stored-state path — was wrong. Empirical reading of
   `grafana/fleet-management/pkg/discovery/v1/prometheus.go:204-306`
   confirmed `extractSurveyInfoClusters` gates the cluster set on the
   `survey_info{}` Prometheus metric; clusters with no `survey_info` are
   absent from the RPC response, not present with `PENDING_INSTRUMENTATION`.
2. **Read-paths table updated.** `clusters list`, `clusters get`,
   `status`, and `clusters wait` now require a merge with
   `PipelineService.ListPipelines` filtered by K8s monitoring pipeline
   metadata so pre-Alloy clusters appear with status
   `PENDING_INSTRUMENTATION`.
3. **Read-path-split prose tightened.** The original framing
   ("`get`/`list` are declared, `status`/`wait` are observed") was
   technically correct in spirit but read as an absolute prohibition on
   cross-reference. The corrected framing acknowledges that `list`/`get`
   may consult observed-state for STATUS only and `ListPipelines` for
   enumeration fallback, while preserving the no-collapse rule (declared
   writes never lag through an observed read; `list` enumeration agrees
   with `get` lookup).
4. **Bug taxonomy unaffected.** B-531-02, -05, -08 root-cause analysis
   stands. The merge fix means `list` now positively surfaces audience-A's
   just-configured cluster (rather than rendering it invisible); without
   the merge the post-redesign `list` would have re-introduced a
   B-531-05-style "list returns [] for a cluster that get returns" symptom
   in the pre-Alloy window. The merge closes that gap by construction.
5. **Status moved `proposed` → `accepted`** to reflect that the design is
   locked.

### What did NOT change

- The action-verb command tree, audience locking (A primary, C secondary,
  B deferred), bare-domain-type output (no K8s envelope), no-GVK
  registration, tri-state flag semantics, optimistic-lock conflict
  surfacing, and the `setup` wizard contract are unchanged from the
  original ADR.
- Rejected alternatives (Design A, tactical CRUD-with-fixes, hybrid,
  ADR-014 setup framework) remain rejected for the same reasons.

## Amendments — 2026-04-30 (post-smoke-test)

The following amendments were applied based on findings from the PR #597 pre-merge smoke test
against the `igorgcxtest` stack (gcx build `296a8339`).

### A — Helm bootstrap

- Chart switched from `grafana/k8s-monitoring` to `grafana/grafana-cloud-onboarding` (v0.4.x).
- The new chart deploys an `alloy-daemon` collector that connects to Fleet Management;
  Fleet Management pushes per-signal collector pipelines (K8s monitoring, Beyla, Kepler).
- New four-key helm value schema: `cluster.name`, `grafanaCloud.fleetManagement.url`,
  `grafanaCloud.fleetManagement.auth.username`, `grafanaCloud.fleetManagement.auth.password`.
- New `FleetManagement` type and `FleetManagementFromStack` helper in the instrumentation client.
  `BackendURLs` is no longer passed to the helm formatter (only to API call sites).

### B — Cross-cutting cluster visibility

- `k8sMonitoringClusterName` filter accepts both `metadata["cluster_name"]` (grafana-cloud-onboarding v0.4.x)
  and `metadata["cluster"]` (legacy), with `cluster_name` taking precedence.
- `clusters wait` pre-flight changed: from `ListPipelines` pipeline-existence check to
  `GetK8SInstrumentation` declared-state check. Pipeline materialization is the wait condition,
  not a pre-flight gate. Undeclared cluster → fail-fast with `DetailedError`; declared cluster
  with no pipeline yet → polls until timeout.

### C — Agent-mode contracts

- `fleet.HTTPError` typed error added. All non-2xx responses from the instrumentation client
  are now wrapped with `fleet.HTTPError`, enabling the `convertFleetHTTPErrors` converter to
  produce actionable `DetailedError` output for HTTP 401 (authentication failed) and
  HTTP 403 (authorization failed: scope insufficient).
- `MutationResult` type added to `internal/providers/instrumentation/output/`. All mutation
  commands emit JSON in agent mode and a one-line summary in non-agent mode. Idempotent
  no-ops emit `{"changed": false}`.
- `apps get` returns a single JSON object (not an array).
- `FieldSelectCodec` for plain Go slices now preserves array shape (`[...]`) instead of
  wrapping in `{"items":[...]}`.
- Agent-mode hint banner (`"hint: use --json list..."`) was previously emitted in agent mode
  only; it has been removed entirely (no hint in any mode).
- `--json list` field discovery now works on empty typed slices via reflection-based field
  enumeration (`reflectFields`).

### D — CLI ergonomics

- `clusters enable` → `clusters configure`. Two modes:
  - `--use-defaults --yes`: apply FR-040 canonical defaults (destructive).
  - `--<feat>[=true|false]` (one or more): incremental RMW, unspecified flags preserved.
  - Combining `--use-defaults` with feature flags is an error.
- `clusters disable` → `clusters remove`.
- `clusters reset` → deleted; folded into `clusters configure --use-defaults --yes`.
- Same renames for `clusters apps` subtree (`apps enable` → `apps configure`, etc.).
- `apps configure --use-defaults --yes` applies all-on canonical defaults:
  `tracing=true, logging=true, processMetrics=true, extendedMetrics=true, profiling=true`.
- `setup` flags renamed: `--yes` → `--use-defaults`; `--cost/--no-cost` →
  `--costmetrics/--no-costmetrics`; `--events/--no-events` → `--clusterevents/--no-clusterevents`;
  `--energy/--no-energy` → `--energymetrics/--no-energymetrics`; `--logs/--no-logs` →
  `--nodelogs/--no-nodelogs`.
- `services exclude/include/clear` now validate workload existence via `RunK8sDiscovery`
  before writing. Missing workload → `DetailedError` with `services list` suggestion.
- JSON response field names use camelCase: `costMetrics`, `clusterEvents`, `energyMetrics`,
  `nodeLogs`, `processMetrics`, `extendedMetrics`, `autoInstrument`.

### E — Selection enum hygiene

Wire-fixture tests confirm the client mapper and request construction correctly handle
`SELECTION_EXCLUDED`. If the live stack returns `""` after a remove operation, the bug
is server-side and requires a fleet-management fix.

## Amendments — Iteration 2 (2026-05-02, PR #597 iteration-2 bugfixes)

### Amendment E — Flag casing migration to kebab-case

Multi-word instrumentation flags migrated from lowercase-concat to kebab-case:
`--costmetrics` → `--cost-metrics`, `--clusterevents` → `--cluster-events`,
`--energymetrics` → `--energy-metrics`, `--nodelogs` → `--node-logs`,
`--processmetrics` → `--process-metrics`, `--extendedmetrics` → `--extended-metrics`.
Note: Amendment D's commit message (`ad1236e6`) claimed "camelCase" but shipped lowercase-concat;
Amendment E corrects to kebab-case.

### Amendment F — Wait predicate uses typed classifier

`clusters wait` and `apps wait` now use `ClassifyK8sMonitoringStatus` /
`ClassifyInstrumentationStatus` to match full proto enum wire names
(`K8S_MONITORING_STATUS_INSTRUMENTED`, not shorthand `INSTRUMENTED`).

### Amendment G — Agent-mode output contract gate

- `--print-helm-only` wraps helm command in `{"helmCommand": "..."}` JSON under agent mode.
- Wait commands emit `WaitResult` JSON under agent mode.
- Error `Details` stripped of box characters in agent-mode JSON envelopes.
- `NormalizeStatus` maps full proto wire names to `OK`/`FAILING`/`NODATA`.
- `--json ?`/`--json list` correctly triggers field discovery for table-default commands.

## Amendments — iteration 3 (2026-05-04)

These amendments correct and extend the iteration-2 decisions based on the iter-3 smoke-test findings (PR #597).

**O: Canonical boolean-flag idiom (`--feat=true|false`)**

The canonical boolean-flag idiom across the instrumentation provider tree is `--feat=true|false`. Setup commands MUST NOT declare `--no-*` paired variants. Configure commands already follow this idiom; setup is migrated to match (Theme O). This eliminates the need for mutually-exclusive flag validation in setup and unifies the surface seen by agents.

**P: Not-found family exits 1 (corrects iter-2 Theme G)**

The iter-2 choice of exit 4 for `clusters get` not-found was a misapplication: `docs/design/exit-codes.md` reserves exit 4 for `ExitPartialFailure` (partial-failure batch operations), not for not-found. The correct exit code for all "Resource not found" errors in the instrumentation tree is **1** (`ExitGeneralError`). The not-found family now consistently exits 1 across `clusters get`, `services include/exclude/clear` workload-not-found, and `apps get` namespace-entirely-unknown. The error envelope (Summary / Details / Suggestions[]) is the canonical `DetailedError` schema; the same schema is applied to the fleet-provider managed-pipeline guard (Theme Q follow-up).

**M.4: Instrumentation list endpoints wrap in `{"items":[]}` envelope**

Instrumentation tree list endpoints now emit the canonical `{"items":[...]}` envelope shape per `docs/design/output.md` § List/collection. Empty results emit `{"items":[]}` (never `null`, never bare `[]`). This aligns the instrumentation tree with the broader CLI (dashboards, folders, K8s-style resources already use this envelope).
