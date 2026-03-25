# gcx↔grafanactl Consolidation Research

*2026-03-18 | Sources: grafana/grafana-cloud-cli, grafana/grafanactl-experiments, CLI collab sync 2026-03-19*

---

## Spike: Consolidation Directions

**OKR:** O2 — GrafanaCON 2026 agentic CLI roadmap
**Status:** Open

### Goal

Try both consolidation directions from the CLI collab sync (2026-03-19 with Ward Bekker and Artur Wierzbicki) to see which sticks:

- **Direction A:** Port `gcx` resources into `grafanactl` as providers
	- https://gist.github.com/wardbekker/0b5914b6871a9248a1abab8c5ea90954 - approach
- **Direction B:** Move `grafanactl` dynamic resource registration into `gcx`
	- https://github.com/grafana/grafana-cloud-cli/compare/main...feat/k8s-migration - PoC
	- https://gist.github.com/wardbekker/51392518685d2b7f8e2e36735d12450a - approach

The outcome should be a clear recommendation (with rationale) for the combined CLI architecture, to inform the GrafanaCON roadmap and the 12-month deprecation plan.

### Context

- CLI naming favored: **grot** (rejected anything `-ctl`)
- Decision from sync: adopt dynamic schema registration model long-term
- Single CLI absorbing both Assistant CLI and grafanactl; 12-month deprecation policy
- Ward is handling GrafanaCON keynote/demo — this spike feeds the architecture decision

### Acceptance Criteria

- [ ] Both directions spiked (even if just partially)
- [ ] Notes captured on what worked, what didn't, and effort estimate per direction
- [ ] Clear recommendation written up for the team

### Related

- [[grafanactl agentic evolution]] — parent theme
- [[Grafana Labs/1-1s/Artur]] — discussed consolidation approach
- [[Fabrizia]] / Ward Bekker — CLI collab sync 2026-03-19

---

## CLI Collab Context — Feature Vote

### What Changed Since the March 13 Executive Brief

The [executive brief](2026-03-13-executive-brief-grafanactl-vs-gcx.md) listed "query engine (PromQL/LogQL/Pyroscope with terminal graph rendering)" as grafanactl's primary moat. That's **no longer accurate**:

**gcx now has full query capability:**
- `gcx metrics query <promql>` — full PromQL (instant + range)
- `gcx logs query <logql>` — full LogQL, plus labels/series/stats/volume/patterns
- `gcx traces get` — Tempo v2 API + LLM trace format
- `gcx profiles query` — Pyroscope
- `gcx datasources query` — arbitrary datasource query via `/api/ds/query`
- `gcx datasources introspect` — inspect datasource schema (metrics, labels)
- `gcx telemetry analyze/diff` — cross-signal analysis, before/after comparison
- `gcx usage` — cost attribution by metric/dashboard/rule (NEW, merged 2026-03-18)

**What grafanactl still has that gcx doesn't:**
- Linter (Rego/OPA) with PromQL/LogQL validators + custom rule authoring, testing, catalog
- Live dev server (`grafanactl serve`) with file-watcher + WebSocket LiveReload
- Code generation (`grafanactl dev generate`) for typed Go resource stubs
- K8s-native client (`k8s.io/client-go`) → watch, discovery, dynamic schema registration via app platform
- Terminal graph rendering in agent-first design (PR #35 adds `graph` + `wide` output modes for SLO/Synth)

### Combined Feature List

Features from the joint planning doc, cleaned up and consolidated:

#### Query & Visualization

| Feature | gcx status | grafanactl status |
|---------|-----------|------------------|
| PromQL / LogQL / Pyroscope queries | ✅ full | ✅ full (refactor in PR #36) |
| ASCII graph output (`-o graph`) | partial (telemetry rendering) | PR #35 adds `graph`/`wide` modes |
| Dashboard snapshots via render | ❌ | PR #34 merged (snapshot cmd) |
| Image interpretation (AI reads graphs) | ❌ | ❌ |

#### Operations & Resource Management

| Feature | gcx status | grafanactl status |
|---------|-----------|------------------|
| Full Grafana Cloud resource CRUD | ✅ 70+ types | ~25 types (K8s API + 3 providers) |
| `push/pull` (apply/export/diff) | ✅ apply/export/diff | ✅ push/pull |
| Plan mode (tf-style dry run) | WIP in doc | ❌ |
| Dynamic schema registry (app platform) | ❌ | Partially (K8s discovery) |
| Linter (Rego/OPA + dashboard rules) | ❌ | ✅ unique |
| `$CLI dev` (FoundationSDK + serve + lint) | ❌ | ✅ serve, generate, lint |

#### Agent Experience

| Feature | gcx status | grafanactl status |
|---------|-----------|------------------|
| Agent-ready JSON output + auto-detection | ✅ JSON envelope | ✅ TTY/pipe detection (PR #31) |
| MCP server | ✅ `gcx mcp serve` | ❌ |
| Annotated command surface (token costs, examples) | ✅ `gcx commands` JSON tree | ❌ |
| Skills plugin distribution | ✅ 11 skills + skills.sh | ✅ 9 Claude Code skills |
| Grafana-debugger agent persona | ❌ | ✅ |
| Schema/example introspection per resource | ✅ `gcx schema/example` | ❌ |

#### Developer & Setup UX

| Feature | gcx status | grafanactl status |
|---------|-----------|------------------|
| Default datasource / directory pinning | partial (init flow) | ✅ per-context config |
| Multi-stack context switching | ✅ `gcx context` | ✅ `--context` flag |
| Keychain support | PR #73 open | ❌ |
| Tiered credential scopes | ✅ | ❌ |
| Backward compat / migration path | N/A | ✅ existing CI users |
| Error handling (unavailable Grafana, 503) | ✅ `gcx doctor` | partial |

### GrafanaCon Context

From the doc, explicit GrafanaCon release goals:
- Joined forces with grafanactl folks / merge with Assistant CLI
- OSS Public Preview checklist
- User-friendly OAuth authZ flow (`gcx new` onboarding)
- 5 min talk track + 2 min demo script
- Product marketing messaging
- Verified command surface + skill quality
- Test infra for API drift detection
- `--help` docs fleshed out with examples

**Non-goals**: Support for pre-2026 releases of self-hosted LGTM or Grafana Cloud.

### Strategic Framing

gcx is mature on breadth + agent protocols (MCP, JSON envelope, A2A, skills). grafanactl is mature on operational depth (query, lint, dev loop, K8s-native). The combined tool needs both, but with 5 weeks to GrafanaCon:

- Can't rebuild everything from scratch
- Must be agent-first, agent-only is OK initially
- Must claim "all of Grafana Cloud" to justify the merge story
- Demo needs to show something *no other Grafana tooling* can do

The dynamic schema registry (app platform auto-discovery) + linter (quality gates) + closed feedback loop (generate → lint → preview → push → verify) is the unique pitch — gcx has breadth but no closed loop; Grafana Assistant has investigation but no write path.

---

## Architecture Comparison — gcx vs grafanactl

### A. Side-by-Side

```
                    gcx                              grafanactl
                    ───                              ──────────
Language            Go 1.25                          Go 1.26
CLI framework       Cobra                            Cobra
Resource model      CRUD[T] generic interface        Provider + ResourceAdapter interface
                    (opt-in, ~13 types conform)      (self-registering via init())
Discovery           None — hand-coded per resource   K8s /apis discovery → dynamic GVKs
Auth model          2-token (AP + GSA), tiered       kubeconfig-style (K8s client-go)
                    scopes, config.yaml              per-context config
Query engine        Unified telemetry.Client          Per-type clients (prometheus/loki/pyro)
                    (Prom/Loki/Tempo/Pyroscope)      dispatched by datasource type
Terminal graphs     telemetry rendering (partial)     ntcharts lib (line + bar charts)
Linter              None                             OPA/Rego engine, extensible
Dev server          None                             Reverse proxy + fsnotify + LiveReload
Code gen            None                             text/template → foundation-sdk stubs
MCP server          ✅ mark3labs/mcp-go, stdio        None
Output formats      text/json/yaml/csv/jsonpath/jq   text/wide/json/yaml + --json ? discovery
Self-observability  Full OTEL SDK (traces+metrics)   None
Skills              11, go:embed in binary            15, separate claude-plugin/ directory
Push/pull           apply/export/diff + --prune       push/pull with processor pipeline
Agent detection     No auto-detect (explicit MCP)     Env-var + --agent flag + TTY detection
```

### B. Resource Registration — The Core Difference

**gcx: hand-coded, flat**
```
cmd/resources/dashboards.go  ← Cobra commands using cmdutil.RunListPaged/RunGet/...
pkg/grafana/dashboards/      ← Client struct, HTTP methods
cmd/cmdutil/permissions.go   ← permission entry
pkg/mcp/registry.go          ← optional MCP registration
```
~150-250 lines per resource. 50+ resources = substantial surface to maintain. No code generation.

**grafanactl: self-registering, layered**
```
internal/providers/slo/provider.go  ← implements Provider interface
  init() → providers.Register() + adapter.Register()
cmd/grafanactl/root/command.go      ← blank import triggers init()
```
~3 files per provider. K8s-discovered resources need zero code — they appear automatically via `/apis` endpoint. Provider resources need an adapter implementing `ResourceAdapter`.

**Verdict**: grafanactl's architecture scales better. Adding a new app platform resource to grafanactl = 0 lines of code. Adding the same to gcx = ~200 lines. For non-app-platform resources (OnCall, k6, etc.), both need similar effort, but grafanactl's `Provider` interface is cleaner than gcx's scattered registration.

### C. Push/Pull Pipeline

**gcx**: `apply` reads a YAML manifest, iterates resources, calls create-or-update per resource. `--prune` deletes resources not in the manifest. `--dry-run` shows what would happen. `diff` compares local vs remote field-by-field. No processor abstraction — transformations are inline.

**grafanactl**: Full pipeline with composable `Processor` interface:
```
push: read local → NamespaceOverrider → ManagerFieldsAppender → router.Create/Update
pull: router.List → ServerFieldsStripper → write local
```
The `ResourceClientRouter` transparently dispatches to provider adapters or K8s dynamic client — command code doesn't care which backend serves a resource.

**Verdict**: grafanactl's pipeline is more extensible. Adding a new transformation (e.g., "strip secrets on pull") = implement one `Processor`. gcx would need surgery on the apply codepath.

### D. Query Engine

**gcx**: Single `telemetry.Client` wrapping all four backends with per-endpoint auth. Snapshot command fires all concurrently. Direct API calls (Prometheus HTTP API, Loki HTTP API, etc.).

**grafanactl**: Separate client packages per type, dispatched by datasource type string in the query command. Routes through Grafana's datasource proxy API (`/api/ds/query`).

**Key difference**: gcx talks directly to backends (needs backend URLs + auth). grafanactl proxies through Grafana (needs only Grafana URL + SA token). grafanactl's approach is simpler for users (one URL, one token) but adds Grafana as a hop.

**Verdict**: Different tradeoffs. gcx is lower latency for bulk telemetry work. grafanactl is simpler to configure. For the combined CLI, grafanactl's proxy approach is better for setup UX; gcx's direct approach is better for performance-sensitive paths. Both approaches should coexist.

### E. Agent & MCP

**gcx MCP server**: 4 generic tools (`list_kinds`, `list_resources`, `get_resource`, `apply_resource`) over stdio. Only 10 of 50+ resource types registered. Uses `CRUD[T]` conformance as the gate.

**grafanactl**: No MCP server. Agent mode is about output formatting (JSON, no color, no truncation) — the agent runs CLI commands in a shell.

**Verdict**: gcx's MCP exists but is thin (10/50 resources). grafanactl's shell-execution model actually works better with current agent runtimes (Claude Code, Codex CLI) which are already shell-native. MCP matters more for GUI agents (Claude Desktop, Cursor). The combined tool needs both paths.

### F. Feature Group Coverage from Code

| # | Feature Group | gcx | grafanactl | Gap |
|---|--------------|-----|-----------|-----|
| 1 | **Telemetry Visualization** | Partial (telemetry rendering) | ✅ ntcharts line/bar + snapshot render | grafanactl leads; gcx needs porting |
| 2 | **Context & Config** | ✅ 2-token + tiers + dir pinning | ✅ kubeconfig-style contexts | gcx richer auth; grafanactl simpler config |
| 3 | **Core Robustness** | ✅ OTEL + audit log + `doctor` | Partial (agent detection only) | gcx leads significantly |
| 4 | **Dynamic Schema Registry** | ❌ hand-coded only | ✅ K8s `/apis` discovery + adapter registry | grafanactl unique advantage |
| 5 | **Agentic UX** | ✅ JSON envelope, `commands` tree, token_cost annotations, `agent-card` | ✅ `--agent` flag, `--json ?` field discovery | gcx leads on annotations; grafanactl leads on field discovery |
| 6 | **o11y-as-Code** | ❌ | ✅ linter + serve + generate | grafanactl unique |
| 7 | **Push/Pull/Plan/Diff** | ✅ apply/export/diff/prune/dry-run | ✅ push/pull with processor pipeline | Both strong; grafanactl more extensible pipeline |
| 8 | **Datasources & Querying** | ✅ all signals + arbitrary ds + analyze/diff + usage | ✅ Prom/Loki/Pyro via proxy + graph output | gcx broader; grafanactl has graph rendering |
| 9 | **Alerting** | ✅ CRUD + silences + contacts + policies | ✅ rules + groups (provider) | gcx broader; grafanactl has dedicated provider |
| 10 | **Product Coverage** | ✅ ~50 resource types | ~6 dedicated + dynamic K8s discovery | gcx leads on breadth; grafanactl leads on scalability |
| 11 | **Skills & Plugins** | ✅ 11 skills + MCP server | ✅ 15 skills + debugger agent + reference docs | grafanactl skills are richer; gcx has MCP |

### G. Merge Direction Analysis

#### Option 1: gcx absorbs grafanactl

**What moves**: linter, dev server, code gen, graph rendering, provider model, K8s discovery, processor pipeline, claude-plugin skills.

| Pro | Con |
|-----|-----|
| gcx already has 50+ resources — immediate breadth | gcx's flat resource model fights grafanactl's layered approach |
| MCP server already exists | Porting K8s discovery into gcx means adding `k8s.io/client-go` — massive dep |
| Agent annotations (token_cost, llm_hint) already in place | gcx has no processor pipeline — push/pull would need significant refactoring |
| Existing CI users of gcx are already on this binary | Linter integration requires OPA dep + bundle system — another large dep |
| | Binary already bloated (OTEL + Prometheus deps); K8s + OPA makes it worse |
| | `Env` struct migration is incomplete (globals bridge still exists) |

**Effort estimate**: High. Porting grafanactl's depth features into gcx's flat architecture means either (a) refactoring gcx to have a proper provider/pipeline model (large), or (b) bolting on the features without the pipeline (results in two inconsistent patterns).

#### Option 2: grafanactl absorbs gcx

**What moves**: 40+ resource clients, MCP server, agent annotations, tiered auth, OTEL self-observability, audit system, skill distribution.

| Pro | Con |
|-----|-----|
| grafanactl's provider model can absorb gcx resources cleanly — each becomes a provider or adapter | 40+ resource clients = weeks of porting |
| K8s discovery + dynamic schema already exists — new app platform resources auto-appear | gcx's direct-to-backend query clients need adaptation to grafanactl's proxy model |
| Pipeline architecture (processors) already handles push/pull properly | gcx users need migration path |
| Linter + dev server + code gen stay in place (no porting needed) | MCP server needs rewiring to use grafanactl's adapter model |
| Binary starts leaner (no OTEL by default) | Tiered auth is a significant porting effort |
| `init()` self-registration means new providers are isolated — less merge conflict risk | |

**Effort estimate**: Medium-high, but incremental. Each gcx resource can be ported as an independent provider without touching existing code. The pipeline and discovery infrastructure already exists.

#### Option 3: New repo, cherry-pick from both

| Pro | Con |
|-----|-----|
| Clean architecture from day 1 | Loses all existing CI users of both tools |
| No legacy patterns to fight | Doubles effort — rebuilding infrastructure that already works |
| Can pick the best pattern for each feature | No time for this with 5 weeks to GrafanaCon |

**Not viable given the timeline.**

### H. What Each Direction Requires Porting

**If grafanactl absorbs gcx — gcx assets to port:**
- **40+ resource clients** — OnCall (5 types), k6 (2), Adaptive Metrics/Logs/Traces (3), Fleet (2), Faro, Cloud Provider (3), Connections, Integrations, KG, ML (2), Recording Rules (2), Annotations, Library Panels, Playlists, Plugins, Teams, Users, Correlations, Access Policies, RBAC, OAuth/SAML/SSO (3), SCIM (2), Secrets, Stacks/Billing/Quotas (3), Cloud Migrations, Assistant
- **`gcx init` onboarding workflow** — bootstrap token → create AP + GSA tokens → configure context → verify connectivity
- **Tiered auth model** — readonly / telemetry / stack-admin / cloud-admin scopes + 2-token architecture (AP + GSA)
- **Agent annotations** — `token_cost`, `llm_hint`, `cloud_only` on all commands
- **`agent-card` / `commands`** — JSON discovery endpoints for agent capability catalog
- **MCP server** — rewire to use ResourceAdapter model (potentially better coverage than gcx's 10/50)
- **Direct-to-backend query paths** — Prom/Loki/Tempo/Pyro without Grafana proxy hop
- **Self-observability** — OTEL traces + metrics (optional via build tag to avoid bloat)
- **Audit log system** — local audit trail per command
- **`gcx doctor`** — health check / connectivity diagnostics
- **Diff command** — field-level +/-/~ diff markers
- **Apply --prune** — convergence mode (delete resources not in manifest)

**If gcx absorbs grafanactl — grafanactl assets to port:**
- **K8s dynamic discovery** — `ResourceAdapter` + `ResourceClientRouter` + `/apis` discovery (or fundamentally restructure gcx's flat resource model)
- **Processor pipeline** — NamespaceOverrider, ManagerFieldsAppender, ServerFieldsStripper
- **OPA/Rego linter** — engine + built-in rule bundle + custom rule loading + OPA test runner
- **Dev server** — reverse proxy + fsnotify file watcher + WebSocket LiveReload + dashboard API intercept
- **Code generation** — text/template → grafana-foundation-sdk typed Go stubs
- **ntcharts terminal graph rendering** — line/bar charts + lipgloss layout
- **`--json ?` field discovery** — recursive JSON path enumeration
- **15 Claude Code skills** + debugger agent + per-skill reference docs
- **`k8s.io/client-go` + OPA dependencies** — significant binary size impact on already-bloated gcx

### I. Architectural Arguments

| Factor | Favors grafanactl as base | Favors gcx as base |
|--------|--------------------------|-------------------|
| Extensibility model | ✅ Provider + adapter + K8s discovery scales to "all of Grafana Cloud" | gcx's flat model needs ~200 LOC per new resource forever |
| Depth features | ✅ Linter, dev server, graph, pipeline stay in place | Would all need porting into gcx's architecture |
| Breadth features | 40+ resource clients need porting (incremental, isolated) | ✅ Already has 50+ resources in place |
| Agent surface | MCP can auto-expose all adapters | ✅ MCP + annotations + agent-card already exist |
| Binary size | ✅ Leaner starting point | Already includes OTEL + Prometheus deps |
| App platform future | ✅ New resources auto-appear via K8s discovery | New resources require manual coding |
| OSS / brand | ✅ grafanactl is the OSS/official CLI — public brand to build on | gcx is a private repo (internal only) |
| Init/onboarding UX | Would need gcx's init flow ported | ✅ Already has `gcx init` + tiered auth |

**The merge decision should follow the P0/P1/P2 vote** — if depth features (linter, dev server, graph) rank P0, that favors grafanactl as base. If breadth + agent surface rank P0, the calculus shifts toward gcx.

---

## Migration Plan: Re-implementing Resources Using gcx Patterns

### Context

grafanactl-experiments (~20k LOC) manages Grafana 12+ resources via the k8s-compatible API using `k8s.io/client-go/dynamic`. grafana-cloud-cli (gcx) manages the same resources via Grafana's REST API with typed HTTP clients. The goal is to port gcx's resource implementations into grafanactl following the provider pattern already established by the synth provider.

**Primary goal: Maintainability and extensibility.** Make it easy to add new Grafana Cloud resources without duplicating boilerplate. The k8s dynamic client stays (auto-discovery), but new resources follow the provider/adapter pattern.

### The Synth Provider as Template

The Synthetic Monitoring provider (`internal/providers/synth/`) is the reference implementation. Every new resource ported from gcx should follow this pattern:

#### File Structure (per provider)
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

#### Registration Pattern (from synth `init()`)
1. `providers.Register(&Provider{})` — makes provider discoverable, adds CLI commands
2. `adapter.Register(Registration{...})` — registers resource adapter for `grafanactl resources get/push/pull/delete` commands

#### Per-Resource Implementation (~300-400 LOC today)
| File | LOC | Purpose |
|------|-----|---------|
| `types.go` | ~50-90 | API types + user-facing spec type |
| `client.go` | ~100-150 | HTTP client (List/Get/Create/Update/Delete) |
| `adapter.go` | ~80-120 | ToResource/FromResource (builds k8s envelope, handles ID-name mapping) |
| `resource_adapter.go` | ~100-150 | CRUD via `unstructured.Unstructured`, calls client + adapter |
| `commands.go` | ~100-200 | Provider-specific commands (list, get, status, etc.) |

#### The Painful Part: `adapter.go` + `resource_adapter.go`
Every resource must manually convert between typed Go structs and `unstructured.Unstructured`:
- **ToResource**: `json.Marshal(spec)` then `json.Unmarshal` to `map[string]any` then wrap in k8s envelope with apiVersion/kind/metadata/spec
- **FromResource**: extract spec map then `json.Marshal` then `json.Unmarshal` to typed struct
- **ID management**: embed numeric IDs in metadata.name (e.g., `"slug-<id>"`) and recover them on read
- **Cross-references**: resolve names to IDs (e.g., probe names to probe IDs in synth)

This is ~200 LOC of boilerplate per resource that a `TypedResourceAdapter[T]` generic could reduce to ~30 LOC.

### Resource Inventory: What Needs Porting from gcx

#### Already in grafanactl (3 providers)
- **Synthetic Monitoring** -- checks + probes (full CRUD + status/timeline)
- **SLO** -- definitions + reports
- **Alert Rules** -- rules + groups (read-only)

#### Tier 1: Simple CRUD -- 1-2 days each (~12 resources)
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

#### Tier 2: Standard CRUD + Helpers -- 2-4 days each (~6 resources)
Need additional logic beyond simple CRUD. Dashboards, folders, and datasources already work via k8s dynamic client; the extra features from gcx (version history, render, health checks, correlations) are ported as supplementary provider commands alongside the existing k8s-native CRUD.

| Resource | gcx Client LOC | Special Features | Notes |
|----------|----------------|-----------------|-------|
| Folders | 242 | Hierarchical, GetOrCreate | Already k8s-native; port extras |
| Dashboards | 381 | Version history, render, search | Already k8s-native; port version/render |
| Datasources | 474 | Correlations, health check, query | Already k8s-native; port extra features |
| RBAC | 128 | Role assignments | Enterprise feature |
| SSO/SAML | 276+95 | Auth provider config | Enterprise feature |
| OAuth | 200 | Provider settings | Enterprise feature |

#### Tier 3: Complex Providers -- 4-7+ days each (~8 resources)
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

#### Tier 4: Cloud-Specific Utilities -- 1-2 days each
| Resource | gcx Client LOC | Notes |
|----------|----------------|-------|
| Adaptive Metrics | 117 | Cloud optimization feature |
| Adaptive Logs | 177 | Cloud optimization feature |
| Adaptive Traces | 172 | Cloud optimization feature |
| App O11y | 151 | Application observability |
| Faro | 275 | Frontend analytics |
| Cloud Migrations | 129 | Migration helpers |
| Recording Rules | 334+167 | Sync/provisioning for Prom/Loki rules |

### The Simplification Opportunity: `TypedResourceAdapter[T]`

The biggest maintainability win is eliminating the manual unstructured conversion in every adapter.

#### Before (current synth pattern, per resource)
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

#### After (with TypedResourceAdapter[T])
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

### Architectural Decisions

#### Keep from grafanactl (strong, load-bearing)
- **k8s dynamic client** for native resources (dashboards, folders, etc.) -- gives auto-discovery without per-resource code
- **Selector/Filter/Descriptor model** -- powerful partial-GVK resolution pipeline
- **Discovery system** -- auto-discovers available API groups from `/api` endpoint
- **Push/Pull pipelines** with processors, folder-before-dashboard ordering, summary tracking
- **Config system** -- kubeconfig-style contexts with server/auth/namespace

#### Adopt from gcx
- **`Env` struct** -- per-invocation state (replaces scattered `cmdconfig.Options`)
- **Generic command helpers** -- `RunList`, `RunGet`, `RunDelete`, etc. adapted for grafanactl's selector-based model
- **Richer output system** -- csv, jsonpath, `--field`, `--jq`, envelope structure
- **`TypedResourceAdapter[T]`** generic -- eliminates manual `unstructured.Unstructured` conversion in provider adapters
- **Annotations** -- `token_cost`, `llm_hint` for agent integration
- **Dry-run/diff/read-only** support at command helper level

#### Key Architectural Differences

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

### Where the Complexity Lies

#### 1. The Unstructured Conversion Layer (HIGH complexity)
Every provider adapter must convert between typed Go structs and `unstructured.Unstructured`. This is the biggest source of boilerplate and bugs. The `TypedResourceAdapter[T]` generic is the primary mitigation.

#### 2. Output System Migration (MEDIUM complexity)
Every command currently registers custom codecs via `opts.IO.RegisterCustomCodec()`. Migrating to a centralized Writer means defining table column configs per resource type (data-driven, not code-driven), replacing `opts.IO.Encode()` calls with `env.Out.Success()`, and touching every command file.

#### 3. Generic Command Helpers (MEDIUM complexity)
grafanactl commands operate on selectors (multi-resource, partial GVK) while gcx operates on single typed resources. The helpers need to bridge this: `RunGet` must handle selector parsing, discovery, filter creation, multi-resource fetching. This is fundamentally more complex than gcx's `RunGet` which just calls `client.GetResource(id)`.

#### 4. Two API Surfaces (LOW-MEDIUM complexity)
k8s-native resources use dynamic client; provider resources use adapters. The `ResourceClientRouter` already handles this cleanly. Main work: making the router work with the new `Env`/helper patterns.

### Phased Approach

#### Phase 0: `TypedResourceAdapter[T]` Generic
**Effort: 1-2 weeks**

Build the generic adapter that auto-handles unstructured conversion, then refactor the existing synth/SLO/alert providers to use it. This proves the pattern works before porting new resources.

- `internal/resources/adapter/typed.go` -- generic adapter
- Refactor `internal/providers/synth/checks/` to use it (validation)
- Refactor `internal/providers/slo/` to use it
- Refactor `internal/providers/alert/` to use it

#### Phase 1: Port Tier 1 Resources (Simple CRUD)
**Effort: 2-3 weeks (12 resources x 1-2 days)**

Each resource follows the synth template but with `TypedResourceAdapter[T]`:
- `types.go` + `client.go` (ported from gcx's `pkg/grafana/{resource}/`)
- Registration via `TypedRegistration[T]` (~30 LOC)
- Optional provider-specific commands

Start with Playlists (simplest) as the first port to validate the pattern.

#### Phase 2: Port Tier 2 Resources (Standard + Helpers)
**Effort: 2-3 weeks**

Folders/Dashboards/Datasources already work via k8s dynamic client. Port the extra features from gcx (version history, render, health checks, correlations) as provider commands alongside existing k8s-native CRUD.

RBAC, SSO/SAML, OAuth are enterprise features -- port as standalone providers.

#### Phase 3: Port Tier 3 Resources (Complex Providers)
**Effort: 6-10 weeks (prioritize by user demand)**

OnCall is the biggest (~1400 LOC client, 12 sub-resources). Suggested priority:
1. OnCall (most requested by users)
2. K6 (testing workflows)
3. Fleet/Alloy (agent management)
4. Telemetry, KG, ML, SCIM, GCom as needed

Each complex provider follows the synth pattern: single provider with multiple sub-resource packages.

#### Phase 4: Port Tier 4 Resources (Cloud Utilities)
**Effort: 1-2 weeks**

Adaptive Metrics/Logs/Traces, App O11y, Faro, Cloud Migrations, Recording Rules.

#### Phase 5: Infrastructure Improvements
**Effort: 2-3 weeks**

- `Env` struct for per-invocation state (`cmd/grafanactl/cmdutil/env.go`)
- Generic command helpers `RunGet`, `RunList`, etc. (`cmd/grafanactl/cmdutil/run.go`)
- Richer output system: csv, jsonpath, `--field`, `--jq` (`internal/output/writer.go`)
- Verb-first routing: `grafanactl get dashboards` alongside `grafanactl resources get dashboards`
- Command annotations: `token_cost`, `llm_hint`
- Read-only mode flag

### Effort Summary

| Phase | Resources | Effort |
|-------|-----------|--------|
| 0: TypedResourceAdapter[T] + refactor existing | 3 (synth, SLO, alert) | 1-2 weeks |
| 1: Tier 1 simple CRUD | 12 resources | 2-3 weeks |
| 2: Tier 2 existing resource extras | 6 resources | 2-3 weeks |
| 3: Tier 3 complex providers | 8 providers | 6-10 weeks |
| 4: Tier 4 cloud utilities | 7 resources | 1-2 weeks |
| 5: Infrastructure improvements | Env, helpers, output, verb-first | 2-3 weeks |
| **Total** | **~39 resources** | **14-23 weeks** |

### Maintainability Impact

After migration, adding a new Grafana Cloud resource requires:

| Today | After Migration |
|---|---|
| ~150 LOC command file with manual codec registration | ~30 LOC command using generic helper |
| ~150 LOC adapter with manual unstructured conversion | ~30 LOC adapter wiring typed functions to `TypedResourceAdapter[T]` |
| Custom output formatting per command | Automatic text/json/yaml/csv/jsonpath via Writer |
| Copy-paste config loading pattern | `env := EnvFromCmd(cmd)` one-liner |

**Net effect**: Adding a new resource type drops from ~400 LOC of boilerplate to ~80 LOC of resource-specific logic.

### Verification

- `make all` passes at each phase (lint + tests + build + docs)
- Existing synth/SLO/alert providers work identically after TypedResourceAdapter refactor
- New providers pass `grafanactl resources get {resource}` / push / pull / delete
- Provider-specific commands (status, timeline, etc.) work
- Agent mode detection and output formatting preserved
