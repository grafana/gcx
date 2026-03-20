# Architecture Comparison — gcx vs grafanactl

*2026-03-18 | Companion to [[2026-03-18-feature-vote-proposal]]*
*Sources: [grafana-cloud-cli](https://github.com/grafana/grafana-cloud-cli), [grafanactl-experiments](https://github.com/grafana/grafanactl-experiments)*

---

## A. Architectural Comparison

### Side-by-Side

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

### Resource Registration — The Core Difference

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

### Push/Pull Pipeline

**gcx**: `apply` reads a YAML manifest, iterates resources, calls create-or-update per resource. `--prune` deletes resources not in the manifest. `--dry-run` shows what would happen. `diff` compares local vs remote field-by-field. No processor abstraction — transformations are inline.

**grafanactl**: Full pipeline with composable `Processor` interface:
```
push: read local → NamespaceOverrider → ManagerFieldsAppender → router.Create/Update
pull: router.List → ServerFieldsStripper → write local
```
The `ResourceClientRouter` transparently dispatches to provider adapters or K8s dynamic client — command code doesn't care which backend serves a resource.

**Verdict**: grafanactl's pipeline is more extensible. Adding a new transformation (e.g., "strip secrets on pull") = implement one `Processor`. gcx would need surgery on the apply codepath.

### Query Engine

**gcx**: Single `telemetry.Client` wrapping all four backends with per-endpoint auth. Snapshot command fires all concurrently. Direct API calls (Prometheus HTTP API, Loki HTTP API, etc.).

**grafanactl**: Separate client packages per type, dispatched by datasource type string in the query command. Routes through Grafana's datasource proxy API (`/api/ds/query`).

**Key difference**: gcx talks directly to backends (needs backend URLs + auth). grafanactl proxies through Grafana (needs only Grafana URL + SA token). grafanactl's approach is simpler for users (one URL, one token) but adds Grafana as a hop.

**Verdict**: Different tradeoffs. gcx is lower latency for bulk telemetry work. grafanactl is simpler to configure. For the combined CLI, grafanactl's proxy approach is better for setup UX; gcx's direct approach is better for performance-sensitive paths. Both approaches should coexist.

### Agent & MCP

**gcx MCP server**: 4 generic tools (`list_kinds`, `list_resources`, `get_resource`, `apply_resource`) over stdio. Only 10 of 50+ resource types registered. Uses `CRUD[T]` conformance as the gate.

**grafanactl**: No MCP server. Agent mode is about output formatting (JSON, no color, no truncation) — the agent runs CLI commands in a shell.

**Verdict**: gcx's MCP exists but is thin (10/50 resources). grafanactl's shell-execution model actually works better with current agent runtimes (Claude Code, Codex CLI) which are already shell-native. MCP matters more for GUI agents (Claude Desktop, Cursor). The combined tool needs both paths.

---

## B. Feature Group Coverage from Code

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

---

## C. Merge Direction Analysis

### Option 1: gcx absorbs grafanactl

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

### Option 2: grafanactl absorbs gcx

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

### Option 3: New repo, cherry-pick from both

| Pro | Con |
|-----|-----|
| Clean architecture from day 1 | Loses all existing CI users of both tools |
| No legacy patterns to fight | Doubles effort — rebuilding infrastructure that already works |
| Can pick the best pattern for each feature | No time for this with 5 weeks to GrafanaCon |

**Not viable given the timeline.**

---

### What Each Direction Requires Porting

The merge direction decision depends on feature prioritization (P0/P1/P2 vote). Here's the porting cost for each direction to inform that decision.

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

### Architectural Arguments (Not a Recommendation Yet)

These are structural observations to weigh alongside the feature prioritization:

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
