gcx/grafanactl conversion

# Re-implementing grafanactl Resources Using gcx Patterns

## Context

grafanactl-experiments (~20k LOC) manages Grafana 12+ resources via the k8s-compatible API using `k8s.io/client-go/dynamic`. grafana-cloud-cli (gcx) manages the same resources via Grafana's REST API with typed HTTP clients. The goal is to port gcx's resource implementations into grafanactl following the provider pattern already established by the synth provider.

**Primary goal: Maintainability and extensibility.** Make it easy to add new Grafana Cloud resources without duplicating boilerplate. The k8s dynamic client stays (auto-discovery), but new resources follow the provider/adapter pattern.

---

## The Synth Provider as Template

The Synthetic Monitoring provider (`internal/providers/synth/`) is the reference implementation. Every new resource ported from gcx should follow this pattern:

### File Structure (per provider)
```
internal/providers/{name}/
├── provider.go              # Provider interface impl, configLoader, init() registration
├── {resource}/
│   ├── types.go             # API structs + user-facing spec structs
│   ├── client.go            # HTTP client wrapping REST API
│   ├── adapter.go           # ToResource/FromResource conversion
│   ├── resource_adapter.go  # ResourceAdapter impl (CRUD via unstructured)
│   ├── commands.go          # Provider-specific CLI commands (optional)
│   └── *_test.go
└── {name}cfg/
    └── loader.go            # Config interfaces + error handling
```

### Registration Pattern (from synth `init()`)
1. `providers.Register(&Provider{})` — makes provider discoverable, adds CLI commands
2. `adapter.Register(Registration{...})` — registers resource adapter for `grafanactl resources get/push/pull/delete` commands

### Per-Resource Implementation (~300-400 LOC today)
| File | LOC | Purpose |
|------|-----|---------|
| `types.go` | ~50-90 | API types + user-facing spec type |
| `client.go` | ~100-150 | HTTP client (List/Get/Create/Update/Delete) |
| `adapter.go` | ~80-120 | ToResource/FromResource (builds k8s envelope, handles ID-name mapping) |
| `resource_adapter.go` | ~100-150 | CRUD via `unstructured.Unstructured`, calls client + adapter |
| `commands.go` | ~100-200 | Provider-specific commands (list, get, status, etc.) |

### The Painful Part: `adapter.go` + `resource_adapter.go`
Every resource must manually convert between typed Go structs and `unstructured.Unstructured`:
- **ToResource**: `json.Marshal(spec)` then `json.Unmarshal` to `map[string]any` then wrap in k8s envelope with apiVersion/kind/metadata/spec
- **FromResource**: extract spec map then `json.Marshal` then `json.Unmarshal` to typed struct
- **ID management**: embed numeric IDs in metadata.name (e.g., `"slug-<id>"`) and recover them on read
- **Cross-references**: resolve names to IDs (e.g., probe names to probe IDs in synth)

This is ~200 LOC of boilerplate per resource that a `TypedResourceAdapter[T]` generic could reduce to ~30 LOC.

---

## Resource Inventory: What Needs Porting from gcx

### Already in grafanactl (3 providers)
- **Synthetic Monitoring** -- checks + probes (full CRUD + status/timeline)
- **SLO** -- definitions + reports
- **Alert Rules** -- rules + groups (read-only)

### Tier 1: Simple CRUD -- 1-2 days each (~12 resources)
These map directly to the synth template with minimal special logic.

| Resource | gcx Client LOC | Special Features | Notes |
|----------|----------------|-----------------|-------|
| Annotations | 123 | -- | Basic CRUD |
| Playlists | 135 | -- | Basic CRUD |
| Snapshots | 107 | -- | Basic CRUD |
| Library Panels | 171 | -- | Basic CRUD |
| Public Dashboards | 118 | -- | Basic CRUD |
| Reports | 119 | -- | Basic CRUD |
| Query History | 118 | -- | Basic CRUD |
| Users | 90 | Add/Get/List only | No update/delete |
| Teams | 245 | Upsert, member mgmt | Slightly richer |
| Service Accounts | 222 | Token management | CRUD + tokens |
| Plugins | 163 | Catalog operations | CRUD + catalog |
| Permissions | 94 | Per-dashboard/folder ACLs | Sub-resource pattern |

### Tier 2: Standard CRUD + Helpers -- 2-4 days each (~6 resources)
Need additional logic beyond simple CRUD. Dashboards, folders, and datasources already work via k8s dynamic client; the extra features from gcx (version history, render, health checks, correlations) are ported as supplementary provider commands alongside the existing k8s-native CRUD.

| Resource | gcx Client LOC | Special Features | Notes |
|----------|----------------|-----------------|-------|
| Folders | 242 | Hierarchical, GetOrCreate | Already k8s-native; port extras |
| Dashboards | 381 | Version history, render, search | Already k8s-native; port version/render |
| Datasources | 474 | Correlations, health check, query | Already k8s-native; port extra features |
| RBAC | 128 | Role assignments | Enterprise feature |
| SSO/SAML | 276+95 | Auth provider config | Enterprise feature |
| OAuth | 200 | Provider settings | Enterprise feature |

### Tier 3: Complex Providers -- 4-7+ days each (~8 resources)
Require significant custom logic, multiple sub-resources, or special patterns.

| Resource | gcx Client LOC | Sub-resources | Notes |
|----------|----------------|---------------|-------|
| **OnCall** | 1418 | 12+ (integrations, escalation chains, schedules, shifts, routes, webhooks, alert groups, notification rules, user groups, slack channels, teams) | Largest provider. Iterator pattern for pagination. |
| **K6** | 1097 | Projects, runs, envs, stacks | Multi-tenant (org+stack auth modes) |
| **Fleet/Alloy** | 328+282+270+248 | Pipelines, collectors, instrumentation, discovery | Agent management system |
| **Telemetry** | 1182+133 | Metric queries, log tail streaming | Real-time streaming features |
| **Knowledge Graph** | 694+397 | Datasets, rules, suppressions, relabels | Multi-format upload/validation |
| **ML** | 511 | ML model management | Domain-specific |
| **SCIM** | 584 | SCIM protocol users/groups | Identity provisioning |
| **GCom** | 295+150+150 | Access policies, billing, stacks, regions | Org-level management |

### Tier 4: Cloud-Specific Utilities -- 1-2 days each
| Resource | gcx Client LOC | Notes |
|----------|----------------|-------|
| Adaptive Metrics | 117 | Cloud optimization feature |
| Adaptive Logs | 177 | Cloud optimization feature |
| Adaptive Traces | 172 | Cloud optimization feature |
| App O11y | 151 | Application observability |
| Faro | 275 | Frontend analytics |
| Cloud Migrations | 129 | Migration helpers |
| Recording Rules | 334+167 | Sync/provisioning for Prom/Loki rules |

---

## The Simplification Opportunity: `TypedResourceAdapter[T]`

The biggest maintainability win is eliminating the manual unstructured conversion in every adapter.

### Before (current synth pattern, per resource)
```
resource_adapter.go (~150 LOC):
  - List: client.List() -> ToResource() each -> build UnstructuredList
  - Get: parse slug ID -> client.Get() -> ToResource() -> to Unstructured
  - Create: FromResource() -> resolve cross-refs -> client.Create() -> ToResource()
  - Update: same as Create but with ID extraction
  - Delete: parse slug ID -> client.Delete()

adapter.go (~120 LOC):
  - ToResource: marshal spec -> build map -> wrap in k8s envelope
  - FromResource: extract spec map -> unmarshal to typed struct
  - ID embedding/extraction helpers
```

### After (with TypedResourceAdapter[T])
```go
// ~30 LOC per resource
adapter.Register(adapter.TypedRegistration[CheckSpec]{
    Descriptor: StaticDescriptor(),
    Aliases:    []string{"checks"},
    GVK:        StaticGVK(),
    Factory: func(ctx context.Context) (adapter.TypedCRUD[CheckSpec], error) {
        baseURL, token, ns, err := loader.LoadSMConfig(ctx)
        client := NewClient(baseURL, token)
        return adapter.TypedCRUD[CheckSpec]{
            Namespace: ns,
            ListFn:    func(ctx) ([]CheckSpec, error) { ... },
            GetFn:     func(ctx, name) (*CheckSpec, error) { ... },
            CreateFn:  func(ctx, spec) (*CheckSpec, error) { ... },
            UpdateFn:  func(ctx, name, spec) (*CheckSpec, error) { ... },
            DeleteFn:  func(ctx, name) error { ... },
        }, nil
    },
})
```

The generic handles: JSON marshaling to/from unstructured, k8s envelope wrapping, name/ID management.

**Caveat**: Resources with cross-reference resolution (synth checks needing probe name-to-ID) still need custom logic in their `ListFn`/`CreateFn`, but the conversion boilerplate disappears.

---

## Architectural Decisions

### Keep from grafanactl (strong, load-bearing)
- **k8s dynamic client** for native resources (dashboards, folders, etc.) -- gives auto-discovery without per-resource code
- **Selector/Filter/Descriptor model** -- powerful partial-GVK resolution pipeline
- **Discovery system** -- auto-discovers available API groups from `/api` endpoint
- **Push/Pull pipelines** with processors, folder-before-dashboard ordering, summary tracking
- **Config system** -- kubeconfig-style contexts with server/auth/namespace

### Adopt from gcx
- **`Env` struct** -- per-invocation state (replaces scattered `cmdconfig.Options`)
- **Generic command helpers** -- `RunList`, `RunGet`, `RunDelete`, etc. adapted for grafanactl's selector-based model
- **Richer output system** -- csv, jsonpath, `--field`, `--jq`, envelope structure
- **`TypedResourceAdapter[T]`** generic -- eliminates manual `unstructured.Unstructured` conversion in provider adapters
- **Annotations** -- `token_cost`, `llm_hint` for agent integration
- **Dry-run/diff/read-only** support at command helper level

### Key Architectural Differences

| | grafanactl (current) | gcx (reference) |
|---|---|---|
| **API surface** | k8s `/apis/` endpoints | Grafana REST API |
| **Client** | k8s dynamic client (unstructured) | Typed HTTP clients per resource |
| **Resource model** | `unstructured.Unstructured` wrapping | Typed Go structs per resource |
| **Discovery** | Auto-discovers API groups from server | Hardcoded resource registry |
| **Command pattern** | `opts` struct + hand-written RunE (~150+ LOC each) | Generic helpers (`RunListSimple`, `RunGet`, etc.) (~20-30 LOC each) |
| **Output** | Per-command codec registration (json/yaml/text/wide) | Centralized Writer (json/yaml/text/csv/jsonpath + `--field`/`--jq`) |
| **State** | `cmdconfig.Options` passed to each command constructor | Per-invocation `Env` struct via `EnvFromCmd(cmd)` |
| **Provider resources** | `ResourceAdapter` interface using k8s types | Per-resource client implementing `CRUD[T]` interface |

---

## Where the Complexity Lies

### 1. The Unstructured Conversion Layer (HIGH complexity)
Every provider adapter must convert between typed Go structs and `unstructured.Unstructured`. This is the biggest source of boilerplate and bugs. The `TypedResourceAdapter[T]` generic is the primary mitigation.

### 2. Output System Migration (MEDIUM complexity)
Every command currently registers custom codecs via `opts.IO.RegisterCustomCodec()`. Migrating to a centralized Writer means defining table column configs per resource type (data-driven, not code-driven), replacing `opts.IO.Encode()` calls with `env.Out.Success()`, and touching every command file.

### 3. Generic Command Helpers (MEDIUM complexity)
grafanactl commands operate on selectors (multi-resource, partial GVK) while gcx operates on single typed resources. The helpers need to bridge this: `RunGet` must handle selector parsing, discovery, filter creation, multi-resource fetching. This is fundamentally more complex than gcx's `RunGet` which just calls `client.GetResource(id)`.

### 4. Two API Surfaces (LOW-MEDIUM complexity)
k8s-native resources use dynamic client; provider resources use adapters. The `ResourceClientRouter` already handles this cleanly. Main work: making the router work with the new `Env`/helper patterns.

---

## Phased Approach

### Phase 0: `TypedResourceAdapter[T]` Generic
**Effort: 1-2 weeks**

Build the generic adapter that auto-handles unstructured conversion, then refactor the existing synth/SLO/alert providers to use it. This proves the pattern works before porting new resources.

- `internal/resources/adapter/typed.go` -- generic adapter
- Refactor `internal/providers/synth/checks/` to use it (validation)
- Refactor `internal/providers/slo/` to use it
- Refactor `internal/providers/alert/` to use it

### Phase 1: Port Tier 1 Resources (Simple CRUD)
**Effort: 2-3 weeks (12 resources x 1-2 days)**

Each resource follows the synth template but with `TypedResourceAdapter[T]`:
- `types.go` + `client.go` (ported from gcx's `pkg/grafana/{resource}/`)
- Registration via `TypedRegistration[T]` (~30 LOC)
- Optional provider-specific commands

Start with Playlists (simplest) as the first port to validate the pattern.

### Phase 2: Port Tier 2 Resources (Standard + Helpers)
**Effort: 2-3 weeks**

Folders/Dashboards/Datasources already work via k8s dynamic client. Port the extra features from gcx (version history, render, health checks, correlations) as provider commands alongside existing k8s-native CRUD.

RBAC, SSO/SAML, OAuth are enterprise features -- port as standalone providers.

### Phase 3: Port Tier 3 Resources (Complex Providers)
**Effort: 6-10 weeks (prioritize by user demand)**

OnCall is the biggest (~1400 LOC client, 12 sub-resources). Suggested priority:
1. OnCall (most requested by users)
2. K6 (testing workflows)
3. Fleet/Alloy (agent management)
4. Telemetry, KG, ML, SCIM, GCom as needed

Each complex provider follows the synth pattern: single provider with multiple sub-resource packages.

### Phase 4: Port Tier 4 Resources (Cloud Utilities)
**Effort: 1-2 weeks**

Adaptive Metrics/Logs/Traces, App O11y, Faro, Cloud Migrations, Recording Rules.

### Phase 5: Infrastructure Improvements
**Effort: 2-3 weeks**

- `Env` struct for per-invocation state (`cmd/grafanactl/cmdutil/env.go`)
- Generic command helpers `RunGet`, `RunList`, etc. (`cmd/grafanactl/cmdutil/run.go`)
- Richer output system: csv, jsonpath, `--field`, `--jq` (`internal/output/writer.go`)
- Verb-first routing: `grafanactl get dashboards` alongside `grafanactl resources get dashboards`
- Command annotations: `token_cost`, `llm_hint`
- Read-only mode flag

---

## Effort Summary

| Phase | Resources | Effort |
|-------|-----------|--------|
| 0: TypedResourceAdapter[T] + refactor existing | 3 (synth, SLO, alert) | 1-2 weeks |
| 1: Tier 1 simple CRUD | 12 resources | 2-3 weeks |
| 2: Tier 2 existing resource extras | 6 resources | 2-3 weeks |
| 3: Tier 3 complex providers | 8 providers | 6-10 weeks |
| 4: Tier 4 cloud utilities | 7 resources | 1-2 weeks |
| 5: Infrastructure improvements | Env, helpers, output, verb-first | 2-3 weeks |
| **Total** | **~39 resources** | **14-23 weeks** |

---

## Maintainability Impact

After migration, adding a new Grafana Cloud resource requires:

| Today | After Migration |
|---|---|
| ~150 LOC command file with manual codec registration | ~30 LOC command using generic helper |
| ~150 LOC adapter with manual unstructured conversion | ~30 LOC adapter wiring typed functions to `TypedResourceAdapter[T]` |
| Custom output formatting per command | Automatic text/json/yaml/csv/jsonpath via Writer |
| Copy-paste config loading pattern | `env := EnvFromCmd(cmd)` one-liner |

**Net effect**: Adding a new resource type drops from ~400 LOC of boilerplate to ~80 LOC of resource-specific logic.

---

## Verification

- `make all` passes at each phase (lint + tests + build + docs)
- Existing synth/SLO/alert providers work identically after TypedResourceAdapter refactor
- New providers pass `grafanactl resources get {resource}` / push / pull / delete
- Provider-specific commands (status, timeline, etc.) work
- Agent mode detection and output formatting preserved
