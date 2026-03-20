# gcx → grafanactl Consolidation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Absorb gcx's product breadth and UX features into grafanactl, making it the single Grafana CLI for GrafanaCon 2026.

**Architecture:** grafanactl is the base. gcx resource clients are ported as providers using the existing `Provider` + `ResourceAdapter` pattern. A new `TypedResourceAdapter[T]` generic eliminates per-resource boilerplate. UX improvements (output, agent annotations, init, audit) are layered incrementally.

**Tech Stack:** Go 1.26, Cobra, `k8s.io/client-go`, grafanactl provider/adapter system

**Feature vote source:** [Vote matrix](https://docs.google.com/spreadsheets/d/1aWNtW4521JTk40i0ahxqyDHdkppsrvTuTPPCY4WGPaw/edit)

---

## Priority Map (from team vote)

Everything below is < 2.0 average (GrafanaCon cutoff). Ordered by vote priority, then dependency order within each tier.

| Avg | Feature Group | What It Means For This Plan |
|-----|--------------|----------------------------|
| 0.0 | Product Coverage | Port gcx resource clients as providers (Phase 1-2) |
| 0.0 | Context / Config | Enhance config for Cloud: tiered scopes, dir pinning (Phase 2) |
| 0.0 | Agentic UX | Agent annotations, `commands` JSON tree, agent-card (Phase 2) |
| 0.0 | Absorb Assistant CLI | Post-consolidation — not in this plan |
| 0.0 | Datasources MLTP | Already done. No migration work needed |
| 0.2 | OAuth | Post-consolidation — depends on IAM team + assistant-cli (not in this plan) |
| 0.2 | Datasources OTHER | DatasourceProvider plugin system (Phase 6) |
| 0.5 | Skills/Plugins | Merge gcx skills into grafanactl Claude plugin (Phase 2) |
| 0.8 | Dynamic Schema | Already built. Enhance with `schemas` command (Phase 2) |
| 0.8 | Telemetry Viz | Already built (PR #35). No migration work needed |
| 1.0 | Push/Pull/Diff | Push/pull already works. Add dry-run polish (Phase 2) |
| 1.3 | o11y-as-Code | Already built (lint/serve/generate). No migration work needed |
| 1.5 | Self-monitoring | Audit logs + API call attribution (Phase 2). NO OTEL. |

### Not in this plan (post-consolidation or dropped)

| Item | Reason |
|------|--------|
| Absorb Assistant CLI | Separate effort after gcx+grafanactl align |
| OAuth onboarding flow | Depends on assistant-cli absorption + IAM team |
| OTEL self-observability | Dropped per decision — adds binary bloat |
| MCP server | Dropped per decision — shell-mode sufficient for current agents |
| `get X` verb shortcuts | Dropped per decision — `resources get X` is fine |
| jsonpath / jq output | Dropped — `--json field1,field2` covers the use case |

---

## Phase 0: Foundation — TypedResourceAdapter[T]

**Why first:** Every provider port requires converting between typed Go structs and `unstructured.Unstructured`. Today that's ~200 LOC of boilerplate per resource (see synth checks `adapter.go` + `resource_adapter.go`). The generic reduces this to ~30 LOC of wiring. Pays for itself after 3 resources; we're porting 40+.

**Estimated effort:** 1 week

### Task 0.1: Build TypedResourceAdapter[T]

**Files:**
- Create: `internal/resources/adapter/typed.go`
- Create: `internal/resources/adapter/typed_test.go`

The generic handles:
- JSON marshal/unmarshal between typed struct `T` and `map[string]any`
- K8s envelope wrapping (apiVersion/kind/metadata/spec)
- Name/ID management (embed numeric IDs in metadata.name, recover on read)
- Namespace injection

```go
// TypedRegistration wires a typed CRUD implementation into the adapter system.
type TypedRegistration[T any] struct {
    Descriptor resources.Descriptor
    Aliases    []string
    GVK        schema.GroupVersionKind
    Factory    func(ctx context.Context) (*TypedCRUD[T], error)
}

// TypedCRUD holds typed function pointers for a resource.
type TypedCRUD[T any] struct {
    Namespace  string
    NameFn     func(T) string           // Extract name/ID from typed object
    ListFn     func(ctx context.Context) ([]T, error)
    GetFn      func(ctx context.Context, name string) (*T, error)
    CreateFn   func(ctx context.Context, spec *T) (*T, error)
    UpdateFn   func(ctx context.Context, name string, spec *T) (*T, error)
    DeleteFn   func(ctx context.Context, name string) error
}
```

The generic auto-generates `ResourceAdapter` from `TypedCRUD[T]`:
- `List` → call `ListFn` → marshal each `T` → wrap in k8s envelope → return `UnstructuredList`
- `Get` → call `GetFn` → marshal → wrap → return `Unstructured`
- `Create` → unwrap `Unstructured` → unmarshal to `T` → call `CreateFn` → wrap result
- `Update` → same pattern
- `Delete` → extract name → call `DeleteFn`

**Test:** Unit tests with a mock typed CRUD (e.g. `TestWidget` struct) verifying round-trip through the adapter.

**Verification:** `make tests` passes.

### Task 0.2: Refactor synth checks to use TypedResourceAdapter[T]

**Files:**
- Modify: `internal/providers/synth/checks/resource_adapter.go` (replace manual adapter)
- Modify: `internal/providers/synth/checks/adapter.go` (simplify or remove ToResource/FromResource)
- Keep: `internal/providers/synth/checks/client.go` (unchanged)
- Keep: `internal/providers/synth/checks/types.go` (unchanged)

Replace the hand-written `ResourceAdapter` with `TypedRegistration[CheckSpec]`. The client and types stay exactly the same. Cross-reference resolution (probe name → ID) moves into `ListFn`/`CreateFn` closures.

**Verification:** `make all` passes. `grafanactl synth checks list` returns same output as before.

### Task 0.3: Refactor synth probes, SLO definitions, alert rules/groups

Same pattern as 0.2, applied to:
- `internal/providers/synth/probes/`
- `internal/providers/slo/definitions/`
- `internal/providers/alert/rules/` + `groups/`

**Verification:** `make all` passes. All provider commands produce identical output.

---

## Phase 1: Complex Providers + Cloud Utilities (P0)

**Why first:** These are the real product width gap. None of these have K8s APIs — they all need provider adapters. This is the work that makes grafanactl claim "all of Grafana Cloud."

**Already working via K8s discovery (zero code needed):** 122 resources including Playlists, Snapshots, Plugins, Dashboards, Folders, Datasources, Alert Rules.

**Estimated effort:** 4-6 weeks

### Task 1.1: OnCall provider

`internal/providers/oncall/` — 1418 LOC, 12 sub-resources: integrations, escalation chains, schedules, shifts, routes, webhooks, alert groups, notification rules, user groups, slack channels, teams. Largest single effort (~1 week). Iterator pattern for pagination.

### Task 1.2: Incidents provider

`internal/providers/incidents/` — ~424 LOC commands, uses Grafana IRM plugin API. CRUD on incidents with time-range filtering (`--lookback`, `--from`/`--to`). Grouped under IRM alongside OnCall.

### Task 1.3: K6 provider

`internal/providers/k6/` — 1097 LOC, projects + runs + envs. Multi-tenant auth (org+stack modes).

### Task 1.4: Fleet/Alloy provider

`internal/providers/fleet/` — 1128 LOC combined, pipelines + collectors + instrumentation + discovery.

### Task 1.5: Knowledge Graph provider

`internal/providers/kg/` — 1091 LOC, datasets + rules + suppressions + relabels. Multi-format upload/validation.

### Task 1.6: ML provider

`internal/providers/ml/` — 511 LOC, model management.

### Task 1.7: SCIM provider

`internal/providers/scim/` — 584 LOC, identity provisioning (users + groups).

### Task 1.8: GCom provider

`internal/providers/gcom/` — 595 LOC, access policies + billing + stacks + regions. Org-level management.

### Task 1.9: Cloud utilities

Small scope, ~1 day each:
- Adaptive Metrics (117 LOC), Adaptive Logs (177 LOC), Adaptive Traces (172 LOC)
- App O11y (151 LOC), Faro (275 LOC)
- Cloud Migrations (129 LOC)
- Recording Rules (501 LOC combined)

**Per-task verification:** `make all` passes + `grafanactl resources get {resource}` works.

---

## Phase 2: UX/AX Improvements (P0-P1)

**Estimated effort:** 3-4 weeks (parallelizable with Phase 1)

### Task 2.1: Agent annotations on all commands

**Priority:** P0 (Agentic UX, 0.0 avg)

Add to all Cobra command definitions:
- `token_cost`: "small" | "medium" | "large" — API overhead hint
- `cloud_only`: "true" for Cloud-only commands (k6, adaptive, etc.)

Implementation: annotation constants in `cmd/grafanactl/cmdutil/` + set on each command's `Annotations` map. Port gcx's annotation values.

### Task 2.2: `commands` JSON tree + `agent-card`

**Priority:** P0 (Agentic UX)

- `grafanactl commands` — recursive JSON dump of command tree with annotations, flags, examples
- `grafanactl agent-card` — A2A-compatible agent capability card (name, version, capabilities, skills, auth schemes, output modes)

Port from gcx's `cmd/setup/commands.go` and `cmd/setup/agent_card.go`.

### Task 2.3: Config enhancements for Cloud

**Priority:** P0 (Context / Config)

- **Directory pinning:** Associate working directories with contexts (`.grafanactl/context` or config-level dir pins). Port gcx's `DirPins` concept.
- **Tiered credential metadata:** Track token scopes per context (readonly/telemetry/admin) for future `init` flow. Don't build the full init yet — just the config schema.
- **Token expiry tracking:** Store `token_expires_at` per context, warn when approaching expiry.

### Task 2.4: CSV output codec

**Priority:** P0-P1 (Agentic UX — structured output for scripting)

Add CSV codec to `cmd/grafanactl/io/`. Small effort (~50 LOC) — new struct implementing `format.Codec`, register in `builtinCodecs()`.

### Task 2.5: Audit logging

**Priority:** P1 (Self-monitoring, 1.5 avg — the non-OTEL part)

- Local JSONL audit log at `~/.grafanactl/audit.jsonl`
- Entry: timestamp, action (create/update/delete), resource type, resource ID, context, error
- Rotation: max file size + max files (configurable)
- Hook into push/pull/delete pipelines

Port from gcx's `pkg/audit/audit.go`.

### Task 2.6: API call attribution

**Priority:** P1 (Self-monitoring — the attribution part)

- Set `User-Agent: grafanactl/{version}` on all HTTP requests
- Add `X-Grafana-Source: grafanactl` header for server-side attribution
- This is small — modify the HTTP transport in config and provider clients.

### Task 2.7: Merge gcx skills into grafanactl Claude plugin

**Priority:** P0-P1 (Skills/Plugins)

Audit gcx's 11 skills, identify which add value beyond grafanactl's existing 15:
- Skills that reference gcx-only commands need updating to grafanactl equivalents
- Skills that cover products not yet ported wait until the provider is ported
- Merge non-overlapping skills into `.claude/skills/`

### Task 2.8: `schemas` command enhancement

**Priority:** P0-P1 (Dynamic Schema)

`grafanactl resources schemas` — list all CRUD-enabled resources per context (combines k8s discovery + registered adapters). Show which resources are k8s-native vs adapter-backed.

### Task 2.9: Push/pull dry-run polish

**Priority:** P1 (Push/Pull/Diff)

- Ensure `--dry-run` works consistently across all resource types (k8s-native + provider adapters)
- Add `dryrun.Report()` summary output showing what would be created/updated/deleted
- Manifest format: K8s-only (apiVersion/kind/metadata/spec) — no flat JSON auto-detect

### Task 2.10: Alerting breadth

**Priority:** P0 (Alerting is part of Product Coverage)

Expand the existing `alert` provider with gcx's broader coverage:
- Silence management (create, list, expire)
- Contact point CRUD (currently read-only)
- Notification policy inspection
- Mute timing CRUD

---

## Phase 3: Non-K8s Grafana REST Resources (P0)

**Why after Phase 1-2:** These are core Grafana resources NOT yet on the K8s app platform. They need provider adapters, but they're simpler than complex providers and less urgent — many will migrate to K8s naturally as app platform adoption continues.

**Estimated effort:** 1-2 weeks (9 resources, 0.5-1 day each)

**Pattern per resource:**

```
internal/providers/{provider-name}/
├── provider.go                    # Provider impl + init() registration
├── {resource}/
│   ├── types.go                   # API structs (ported from gcx pkg/grafana/{resource}/)
│   ├── client.go                  # HTTP client (ported from gcx, adapted to grafanactl's config)
│   ├── resource_adapter.go        # TypedRegistration[T] wiring (~30 LOC)
│   └── *_test.go
```

### Task 3.1: Port Annotations (validation resource)

**Source:** `gcx/pkg/grafana/annotations/` (123 LOC client)
**Target:** `internal/providers/grafana/annotations/`
**Provider:** New `grafana` provider (core Grafana resources not yet on K8s API)

Note: int64 IDs — adapter maps numeric ID to metadata.name as string.

### Task 3.2-3.9: Port remaining non-K8s Tier 1 resources

**`grafana` provider** (core Grafana REST resources not yet on K8s API):
- 3.2: Library Panels (171 LOC)
- 3.3: Public Dashboards (118 LOC)
- 3.4: Reports (119 LOC) — note: int IDs
- 3.5: Query History (118 LOC)
- 3.6: Users (90 LOC) — Add/Get/List only
- 3.7: Teams (245 LOC) — includes member management
- 3.8: Service Accounts (222 LOC) — CRUD + token management

**`iam` provider** (identity & access):
- 3.9: Permissions (94 LOC) — per-dashboard/folder ACLs, sub-resource pattern

**Per-task verification:** `make all` passes + `grafanactl resources get {resource}` works.

---

## Phase 4: Existing Resource Extras + Enterprise (P1)

**Why after Phase 3:** K8s CRUD already works for dashboards, folders, datasources. These are supplementary features from gcx (version history, render, health checks) ported as provider commands alongside existing k8s-native CRUD.

**Estimated effort:** 2-3 weeks

### Task 4.1: Dashboard extras

- Version history (`grafanactl dashboards versions`)
- Search

### Task 4.2: Folder extras

- GetOrCreate
- Hierarchical operations

### Task 4.3: Datasource extras

- Correlations
- Health check

### Task 4.4: Enterprise features

Port as standalone providers under `iam`:
- **RBAC** (128 LOC) → `iam` provider
- **SSO/SAML** (276+95 LOC) → `iam` provider
- **OAuth** (200 LOC) → `iam` provider

---

## Phase 5: Init/Onboarding

**Why here:** Depends on config enhancements (Phase 2.3) and having providers ported (Phase 1) so the init flow can verify connectivity to the products it bootstraps.

**Estimated effort:** 1 week

### Task 5.1: `grafanactl init` command

Port gcx's `gcx init` workflow adapted to grafanactl's config system:
1. Prompt for Grafana URL (or accept via flag/env var)
2. Exchange bootstrap token for scoped tokens
3. Create config context with token + server + namespace
4. Verify connectivity (call `resources schemas` to confirm discovery works)

### Task 5.2: Permissions registry

Map commands → required auth scopes/roles (460+ mappings from gcx). Powers:
- Pre-flight "you don't have permission" checks
- `init` knowing which scopes to request per tier
- Agent discovery of command requirements

---

## Phase 6: Delayed (post-GrafanaCon or last)

Implement after Phases 0-5 if time allows.

| Item | Effort | Dependency |
|------|--------|------------|
| DatasourceProvider plugin system (`grafanactl-experiments-e5i`) | 1-2 weeks | After core providers stabilize |
| Doctor diagnostics (100+ endpoint health probes) | 3-5 days | Needs providers ported (Phase 1) |
| Consistency tests (tree-walk convention enforcement) | 2-3 days | After command structure stabilizes |
| Field-level diff (`grafanactl diff`) | 1 week | After push/pull polish (2.9) |
| apply --prune (convergence mode) | 3-5 days | After diff |

---

## Execution Order Summary

```
Phase 0: Foundation (1 week)
├── 0.1 TypedResourceAdapter[T] generic
├── 0.2 Refactor synth checks
└── 0.3 Refactor probes, SLO, alerts

Phase 1: Complex Providers + Cloud Utilities (4-6 weeks)         ←── PRODUCT WIDTH
├── 1.1 OnCall (largest, ~1 week)
├── 1.2 Incidents (IRM)
├── 1.3 K6
├── 1.4 Fleet/Alloy
├── 1.5 Knowledge Graph
├── 1.6 ML
├── 1.7 SCIM
├── 1.8 GCom
└── 1.9 Cloud utilities (Adaptive, Faro, etc.)

Phase 2: UX/AX (3-4 weeks, parallelizable with Phase 1)        ←── UX + AGENT EXPERIENCE
├── 2.1 Agent annotations
├── 2.2 commands + agent-card
├── 2.3 Config enhancements
├── 2.4 CSV output codec
├── 2.5 Audit logging
├── 2.6 API call attribution
├── 2.7 Merge skills
├── 2.8 schemas command
├── 2.9 Dry-run polish
└── 2.10 Alerting breadth

Phase 3: Non-K8s Grafana REST (1-2 weeks)                       ←── LAGGERS
├── 3.1 Annotations (validation port)
└── 3.2-3.9 Library Panels, Public Dashboards, Reports, Query History,
            Users, Teams, Service Accounts, Permissions

Phase 4: Existing Resource Extras (2-3 weeks)                    ←── POLISH
├── 4.1 Dashboard extras (versions, search)
├── 4.2 Folder extras (GetOrCreate, hierarchy)
├── 4.3 Datasource extras (correlations, health)
└── 4.4 Enterprise (RBAC, SSO/SAML, OAuth)

Phase 5: Init/Onboarding (1 week)                               ←── BOOTSTRAP UX
├── 5.1 grafanactl init command
└── 5.2 Permissions registry

Phase 6: Delayed (post-GrafanaCon)
├── DatasourceProvider plugin system
├── Doctor diagnostics
├── Consistency tests
├── Field-level diff
└── apply --prune
```

**Total estimated effort:** 12-17 weeks (Phases 0-5), with Phase 1 and 2 parallelizable.

**GrafanaCon critical path:** Phase 0 (1w) + Phase 1 top-priority providers (3-4w) + Phase 2 key items (2w) = ~6-7 weeks.

---

## Spike Scope (immediate next step)

Before committing to the full plan, validate with a spike:

**Spike = Task 0.1 (TypedResourceAdapter[T]) + one complex provider sub-resource (e.g. OnCall integrations)**

This proves:
1. The generic works and reduces boilerplate
2. A gcx REST-only resource client ports cleanly into grafanactl as a provider
3. The result plugs into `grafanactl resources get/push/pull/delete`
4. Complex provider patterns (separate API domain, custom auth) work through the adapter
5. Gives concrete effort data to refine estimates

**Spike effort:** 2-3 days

---

## Verification Gates

Each phase must pass before proceeding:

- **Phase 0 gate:** `make all` passes. Existing synth/SLO/alert commands produce identical output.
- **Phase 1 gate:** `make all` passes. OnCall/K6/Fleet/etc. resources accessible via `grafanactl resources get`.
- **Phase 2 gate:** `make all` passes. `grafanactl commands` returns valid JSON. Agent annotations present. Audit log written on mutations.
- **Phase 3 gate:** `make all` passes. All 9 non-K8s resources appear in `grafanactl resources get {type}`.
- **Phase 4 gate:** `make all` passes. Dashboard versions, folder hierarchy, DS health all functional.
- **Phase 5 gate:** `grafanactl init` bootstraps a working config from a single token.
