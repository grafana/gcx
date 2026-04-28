# `gcx instrumentation` CLI redesign: action verbs over Set/Get + observed state

**Created**: 2026-04-25
**Last updated**: 2026-04-28
**Status**: accepted
**Supersedes**: [docs/adrs/instrumentation/001-instrumentation-provider-design.md](001-instrumentation-provider-design.md)
**Spec**: [docs/specs/feature-instrumentation-cli-redesign/spec.md](../../specs/feature-instrumentation-cli-redesign/spec.md) (status: approved)

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
| `clusters list` | merge `RunK8sMonitoring()` ⋃ `PipelineService.ListPipelines()` filtered by K8s monitoring metadata (clusters present only in pipeline state appear with status `PENDING_INSTRUMENTATION`); then fan out `GetK8SInstrumentation` per cluster (parallel, capped at 10). See spec FR-018 and Update §2026-04-28 below. |
| `clusters get <c>` | `GetK8SInstrumentation(c)` + cross-ref status from a single `RunK8sMonitoring()`; if `<c>` is absent from `RunK8sMonitoring()`, fall back to `PipelineService.ListPipelines()` to determine status (pipeline present → `PENDING_INSTRUMENTATION`; absent → `NOT_INSTRUMENTED`). See spec FR-019. |
| `clusters apps list <cluster>` | `GetAppInstrumentation(cluster)` (single RPC) |
| `clusters apps get <c> <ns>` | `GetAppInstrumentation(c)` + filter; CLI-level not-found if absent |
| `services list` | `RunK8sDiscovery()` (single RPC, fleet-wide) + client-side filtering |
| `services get <c> <ns> <s>` | `RunK8sDiscovery()` + filter |
| `services include\|exclude\|clear` | `GetAppInstrumentation(c)` → mutate `apps[]` → `SetAppInstrumentation(c, …)` |
| `status` | same cluster set as `clusters list` (per FR-024 — merge with `ListPipelines` so pre-Alloy clusters appear); `RunK8sDiscovery()` when drilling to namespace via `--namespace`. |
| `wait` | poll `RunK8sMonitoring()` at 5s intervals; treat absence from response as `PENDING_INSTRUMENTATION` and continue polling; pre-flight pipeline-existence check (via `GetK8SInstrumentation` or `ListPipelines`) avoids polling-to-timeout when no pipeline exists. See spec FR-046. |

`get`/`list` treat declared-state endpoints (`Get*`, `ListPipelines` for
enumeration fallback) as the source of truth for resource existence and
configuration; observed-state endpoints (`RunK8sMonitoring`) are
cross-referenced for STATUS only. `status`/`wait` go through observed-state
endpoints with the same merge so the cluster set agrees with `list`/`get`.
Both read paths exist because both questions are real ("what did I
configure?" vs "what is Beyla actually seeing?"); collapsing them into
one verb is what produced B-531-02 and B-531-05. The original draft of
this ADR specified single-source enumeration on `list`/`get`; the spec
phase resolved Open Question 1 empirically (see Update §2026-04-28) and
introduced the merge.

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
backward-compatibility aliases are provided. The mapping at a glance:
`clusters create -f` and `clusters update -f` collapse into
`clusters enable [flags]`; `clusters delete` becomes `clusters disable`;
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
  metadata, codified in spec FR-018, FR-019, FR-024, FR-046 and verified
  by AC-021 / AC-021a. The read-paths table above and the Update section
  reflect the resolution.
- **F-AGENT-01..04 cross-cutting fixes** (empty list returns `null`,
  create-error details, agent-mode confirmation prompts, source-file
  mutation) are orthogonal to this redesign and tracked in a separate
  spec. The new commands inherit the cross-cutting fixes when those land.
- **Out-of-scope future work, contingent on backend evolution.**
  - GitOps integration: when fleet-management exposes `Cluster` and `App`
    as true CRUD with `resourceVersion` and a `status` subresource,
    register them in `gcx resources` and add `instrumentation apply -f`.
  - Embedded helm install / access policy mint: tracked in #546.
  - Auto-detection of cluster context from `KUBECONFIG` for `setup`.
  - Per-workload override flags on `apps enable`: revisit if
    `services include/exclude/clear` proves too verbose.

## Update — 2026-04-28 (spec phase)

The spec at
[docs/specs/feature-instrumentation-cli-redesign/spec.md](../../specs/feature-instrumentation-cli-redesign/spec.md)
(plus its companion [plan.md](../../specs/feature-instrumentation-cli-redesign/plan.md))
is now `approved` and is the authoritative source of requirements. This
section records the substantive corrections that landed during spec
authoring against the original draft of this ADR.

### What changed

1. **Open Question 1 resolved (RESOLVED above).** The original
   investigation hypothesis — that `RunK8sMonitoring` surfaces
   `Set`-but-not-Alloy-reporting clusters via the stored-state path —
   was wrong. Empirical reading of
   `grafana/fleet-management/pkg/discovery/v1/prometheus.go:204-306`
   confirmed `extractSurveyInfoClusters` gates the cluster set on the
   `survey_info{}` Prometheus metric; clusters with no `survey_info` are
   absent from the RPC response, not present with `PENDING_INSTRUMENTATION`.
2. **Read-paths table updated.** `clusters list` (FR-018), `clusters get`
   (FR-019), `status` (FR-024), and `clusters wait` (FR-046) now require
   a merge with `PipelineService.ListPipelines` filtered by K8s monitoring
   pipeline metadata so pre-Alloy clusters appear with status
   `PENDING_INSTRUMENTATION`. AC-021 / AC-021a verify both the list and
   get paths against the pre-Alloy window.
3. **Read-path-split prose tightened.** The original framing
   ("`get`/`list` are declared, `status`/`wait` are observed") was
   technically correct in spirit but read as an absolute prohibition on
   cross-reference. The corrected framing acknowledges that `list`/`get`
   may consult observed-state for STATUS only and `ListPipelines` for
   enumeration fallback, while preserving the no-collapse rule (declared
   writes never lag through an observed read; `list` enumeration agrees
   with `get` lookup). Spec NC-08 carries the corrected wording.
4. **Bug taxonomy unaffected.** B-531-02, -05, -08 root-cause analysis
   stands. The merge fix means `list` now positively surfaces audience-A's
   just-enabled cluster (rather than rendering it invisible); without the
   merge the post-redesign `list` would have re-introduced a B-531-05-style
   "list returns [] for a cluster that get returns" symptom in the
   pre-Alloy window. The merge closes that gap by construction.
5. **Status moved `proposed` → `accepted`** to reflect that the spec is
   approved and the design is locked.

### What did NOT change

- The action-verb command tree, audience locking (A primary, C secondary,
  B deferred), bare-domain-type output (no K8s envelope), no-GVK
  registration, tri-state flag semantics, optimistic-lock conflict
  surfacing, and the `setup` wizard contract are unchanged from the
  original ADR. The spec phase ratified them and added implementation-
  level detail, not new design decisions.
- Rejected alternatives (Design A, tactical CRUD-with-fixes, hybrid,
  ADR-014 setup framework) remain rejected for the same reasons.

### Authoritative source going forward

For any current technical claim, the spec is authoritative. This ADR
records the original decision and the empirical correction at spec
phase; future amendments to the design surface should land as new ADRs
(or as further Update sections here) referencing the spec for current
state.
