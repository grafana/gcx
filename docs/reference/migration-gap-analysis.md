# Migration Gap Analysis: grafana-cloud-cli → gcx

## Context
We are migrating from the old `grafana-cloud-cli` to the new `gcx` codebase. This document catalogs every command, flag, and utility present in the old CLI that is **missing or incomplete** in the new one, to guide migration planning.

---

## 1. Setup & Authentication Commands

### Missing Commands

| Old Command | Description | Status in gcx |
|-------------|-------------|---------------|
| `gcx init` | Bootstrap credentials from a Cloud token (tier, ttl, force, print-only) | **Missing entirely** |
| `gcx init destroy` | Remove access policy and delete local config | **Missing** |
| `gcx init self-hosted` | Bootstrap for self-hosted Grafana/LGTM stacks | **Missing** |
| `gcx auth status` | Show auth tier, scopes, token expiry | **Missing** |
| `gcx session start/list/end/cleanup` | Temporary scoped troubleshooting sessions | **Missing** |
| `gcx dir init/show/unpin` | Per-directory context pinning | **Missing** |
| `gcx completion` | Shell completion (bash/zsh/fish/powershell) | **Exists** (`gcx completion bash\|fish\|powershell\|zsh`) |
| `gcx version` | Print version | **Exists** (`gcx --version` flag) |
| `gcx agent-card` | A2A-compliant agent card generation | **Missing** |
| `gcx commands` | List all commands and schemas (LLM-friendly) | **Exists** (`gcx commands --flat -o json`, with `--validate` for live checks) |
| `gcx help-tree` | Compact command tree for LLM/scripting | **Exists** (`gcx help-tree`) |
| `gcx api-resources` | List all known resource types | **Partially exists** (`gcx commands` includes `resource_types` section; `resources schemas` covers K8s types) |
| `gcx explain` | Field-level docs for resource types | **Missing** |
| `gcx doctor` | Validate API endpoint availability | **Missing** |
| `gcx lifecycle` | End-to-end lifecycle tests | **Missing** |
| `gcx skills install/list` | Manage LLM skills | **Missing** |

### Config Differences

| Feature | Old CLI | New gcx |
|---------|---------|---------|
| Config subcommands | `context show/use/list/delete` | `config view/current-context/list-contexts/use-context/set/unset/edit/path/check` |
| Config layers | Single file | System/user/local layers (more advanced) |

**Assessment:** New gcx has a *more sophisticated* config system, `commands` catalog, `help-tree`, and shell completion. Still missing the `init` bootstrap flow, auth status, sessions, and dir pinning.

---

## 2. Global Flags

### Missing Global Flags

| Flag | Description | Notes |
|------|-------------|-------|
| `--token` | Cloud Access Policy token | gcx uses config contexts instead |
| `--stack` | Target stack slug | gcx uses config contexts |
| `--context` | Config context for this invocation | **Exists** (global `--context` flag on root command) |
| `-o, --output` (global) | Global output format (text/json/yaml/csv/jsonpath) | gcx has per-command `-o` but not global |
| `-q, --quiet` | Suppress decorative output | **Missing** |
| `--field` | Extract single field from output | **Missing** |
| `--dry-run` (global) | Global dry-run mode | gcx has it only on `resources push/delete` |
| `--diff` | Show field-level diff with dry-run | **Missing** |
| `--upsert` | Create-or-update on 409 conflict | **Missing** |
| `--if-not-exists` | Skip silently on 409 | **Missing** |
| `--readonly` | Enforce read-only mode | **Missing** |
| `--gcom-url` | GCOM API base URL | **Missing** as flag (may be in config) |
| `--stack-url` | Grafana stack API URL | **Missing** as flag |
| `--no-audit` | Disable local audit log | **Missing** (gcx has no audit log yet) |
| `--self-observe` | Send CLI traces/metrics to stack | **Missing** |
| `--jq` | Apply jq expression to output | **Missing** |
| `--timeout` | Max command duration | **Missing** |

### New gcx-Only Flags
- `--no-color` - Disable color
- `--no-truncate` - Disable table truncation
- `--agent` - Agent mode (auto-detected)
- `-v` (count-based) - Multiple verbosity levels

---

## 3. Verb-First (kubectl-style) Commands

### Missing
| Old Command | Description |
|-------------|-------------|
| `gcx get <resource>[/id]` | Unified get/list any resource |
| `gcx create <resource> -f file` | Unified create from file |
| `gcx update <resource>[/id] -f file` | Unified update from file |
| `gcx delete <resource> <id>` | Unified delete |

**Assessment:** gcx has `resources get/push/pull/delete` which is more powerful (bulk, selector-based), but lacks the simple verb-first shorthand for single resources.

---

## 4. GitOps / Bulk Operations

### Missing
| Old Command | Description | gcx Equivalent |
|-------------|-------------|----------------|
| `gcx export [dir]` | Export resources to disk | `resources pull` (exists) |
| `gcx apply [dir]` | Apply manifests to cloud | `resources push` (exists) |
| `gcx diff [dir]` | Diff local vs remote | **Missing** |
| `gcx audit [resource-type]` | Lint/scan resources in cloud | **Missing** (gcx has `dev lint` for local files only) |
| `gcx analyze metrics-usage` | Extract metric names, cross-ref with Prometheus | **Missing** |
| `gcx status` | Unified health: firing alerts + active incidents + OnCall | **Missing** |
| `gcx summary` | Resource counts across stack | **Missing** |

---

## 5. Grafana Core Resources

### Dashboards

| Feature | Old CLI | New gcx |
|---------|---------|---------|
| `list` | Yes (--limit, --all, --query) | **Missing** |
| `get <uid>` | Yes (--open) | Via `resources get dashboards/<uid>` |
| `create -f` | Yes (--folder-uid, --folder-name) | Via `resources push` |
| `update <uid> -f` | Yes (--folder-uid, --folder-name) | Via `resources push` |
| `delete <uid>` | Yes | Via `resources delete` |
| `versions list <uid>` | Yes (--limit) | **Missing** |
| `render <uid>` | Yes (panel, width, height, from, to, theme) | `dashboards snapshot` (exists, similar flags) |

### Datasources

| Feature | Old CLI | New gcx |
|---------|---------|---------|
| `list` | Yes | `datasources list` (exists) |
| `get <uid>` | Yes (--open) | `datasources get` (exists) |
| `create/update/delete` | Yes | **Missing** (no mutation commands) |
| `health <uid>` | Health check | **Missing** |
| `query <uid>` | Generic query through DS | `datasources generic query` (exists) |
| `query-example <uid>` | Print example query body | **Missing** |
| `describe <uid>` | Show tables/columns for SQL DS | **Missing** |
| Prometheus query | Yes | `datasources prometheus query` (exists) |
| Prometheus labels | **Missing** | `datasources prometheus labels` (NEW in gcx) |
| Prometheus metadata | **Missing** | `datasources prometheus metadata` (NEW in gcx) |
| Prometheus targets | **Missing** | `datasources prometheus targets` (NEW in gcx) |
| Loki query | Yes | `datasources loki query` (exists) |
| Loki labels | **Missing** | `datasources loki labels` (NEW in gcx) |
| Loki series | **Missing** | `datasources loki series` (NEW in gcx) |
| Pyroscope query | Via profiles cmd | `datasources pyroscope query` (exists) |
| Tempo query | Via traces cmd | `datasources tempo query` (exists) |

### Missing Dedicated Resource Commands

| Old Command | Description | Status in gcx |
|-------------|-------------|---------------|
| `gcx folders` | Full CRUD for folders | **K8s tier** (Folder resource: `gcx resources get\|push\|pull\|delete folders`) |
| `gcx folder-permissions` | Manage folder permissions | **Missing** |
| `gcx dashboard-permissions` | Manage dashboard permissions | **Missing** |
| `gcx annotations` | Full CRUD for annotations | **Missing** |
| `gcx teams` | Full CRUD for teams | **Missing** (no K8s resource type) |
| `gcx users` | Full CRUD for users | **Missing** (no K8s resource type) |
| `gcx serviceaccounts` | Full CRUD + token management | **Missing** |
| `gcx plugins` | List, get, enable, disable plugins | **K8s tier** (Plugin resource: `gcx resources get plugins`) |
| `gcx query-history` | Full CRUD for query history | **K8s tier** (Query resource: `gcx resources get queries`) |
| `gcx library-panels` | Full CRUD for library panels | **Missing** |
| `gcx playlists` | Full CRUD for playlists | **K8s tier** (Playlist resource: `gcx resources get playlists`) |
| `gcx snapshots` | List, get, delete snapshots | **K8s tier** (Snapshot resource: `gcx resources get snapshots`) |
| `gcx public-dashboards` | Full CRUD for public dashboards | **Missing** |
| `gcx reports` | Full CRUD for scheduled reports | **Missing** |
| `gcx silences` | Full CRUD for alert silences | **Missing** |
| `gcx correlations` | Full CRUD for datasource correlations | **Missing** |

**Note:** Several resources are now accessible via the K8s resource tier (`gcx resources get/push/pull/delete`). The gap is dedicated shorthand commands with resource-specific flags. Additional K8s-tier resources discovered: SecureValue, Connection, AlertEnrichment, Repository, Job, Keeper, SandboxSettings, AnnouncementBanner.

---

## 6. Observability Commands

### Alerting

| Feature | Old CLI | New gcx |
|---------|---------|---------|
| `alerting rule-groups` CRUD + export | Full | `alert rules/groups` via provider (exists) |
| `alerting contact-points` CRUD + export | Full | **Missing** |
| `alerting notification-policies` get/update/export | Full | **Missing** |
| `alerting mute-timings` CRUD + export | Full | **Missing** |
| `alerting templates` list/silence | Full | **Missing** |
| `alerting alerts` (firing) | Show firing alerts | **Missing** |
| `alerting settings` | Manage settings | **Missing** |
| `alerting overrides` | Provisioning overrides | **Missing** |

### Telemetry Commands

| Old Command | Description | Status in gcx |
|-------------|-------------|---------------|
| `gcx metrics query/range` | PromQL queries (standalone) | **Exists** (`gcx metrics query`, `gcx metrics labels`, `gcx metrics metadata`) |
| `gcx logs query` | LogQL queries (standalone) | **Exists** (`gcx logs query`, `gcx logs labels`, `gcx logs series`) |
| `gcx traces query/search` | TraceQL queries (standalone) | **Exists** (`gcx traces search`, `gcx traces get`, `gcx traces tags`, `gcx traces tag-values`, `gcx traces metrics`) |
| `gcx profiles types/flamegraph/series/label-values` | Pyroscope queries (standalone) | **Exists** (`gcx profiles query`, `gcx profiles labels`, `gcx profiles profile-types`, `gcx profiles series`) |
| `gcx telemetry status` | Pipeline health and ingest rate | **Missing** |
| `gcx telemetry cardinality` | Metric cardinality analysis | **Missing** |
| `gcx telemetry diff` | Before/after cardinality comparison | **Missing** |
| `gcx telemetry verify-pipeline` | Signal pipeline verification | **Missing** |
| `gcx telemetry analyze` | Usage pattern analysis | **Missing** |
| `gcx telemetry queries` | Common queries | **Missing** |

**Note:** gcx now has standalone signal providers (`gcx metrics`, `gcx logs`, `gcx traces`, `gcx profiles`) covering query + adaptive functionality. The `telemetry` analysis suite is still missing.

### Adaptive / Cost Optimization

| Old Command | Description | Status in gcx |
|-------------|-------------|---------------|
| `gcx adaptive-logs exemptions/patterns` | Log reduction/sampling | **Exists** (`gcx logs adaptive exemptions` CRUD, `gcx logs adaptive patterns show\|stats`, `gcx logs adaptive segments` CRUD) |
| `gcx adaptive-metrics rules/recommendations` | Cardinality reduction | **Exists** (`gcx metrics adaptive rules show\|sync`, `gcx metrics adaptive recommendations show\|apply`) |
| `gcx adaptive-traces policies/recommendations/insights/tenants` | Trace sampling | **Partially exists** (`gcx traces adaptive policies` CRUD, `gcx traces adaptive recommendations show\|apply\|dismiss`; insights/tenants **missing**) |
| `gcx adaptive-profiles list/sync` | Profile sampling | **Stub** (`gcx profiles adaptive` exists but no subcommands listed) |
| `gcx recording-rules prometheus/loki` | Recording rules management | **Partially exists** (RecordingRule K8s resource type available via `gcx resources get\|pull\|push`; no dedicated command) |

### Other Observability

| Old Command | Description | Status in gcx |
|-------------|-------------|---------------|
| `gcx connections jobs` | Metric endpoint connections | **K8s tier** (Connection, Job resources: `gcx resources get connections\|jobs`) |
| `gcx frontend sourcemaps` | RUM source map management | **Exists** (`gcx frontend apps` full CRUD + `apply-sourcemap\|remove-sourcemap\|show-sourcemaps`) |
| `gcx integrations` | Telemetry integrations CRUD | **Missing** |
| `gcx app-o11y settings/overrides` | Application Observability | **Exists** (`gcx appo11y settings get\|update`, `gcx appo11y overrides get\|update`) |
| `gcx alloy` | Alloy configuration CRUD | **Partially exists** (`gcx fleet pipelines\|collectors` CRUD; `gcx setup instrumentation status\|discover\|show\|apply`) |
| `gcx otlp-endpoint` | Show OTLP endpoint config | **Missing** |
| `gcx usage get/unused` | Resource usage analysis | **Missing** |
| `gcx billing metrics` | Billing metrics query | **Missing** |

---

## 7. Platform & Security

| Old Command | Description | Status in gcx |
|-------------|-------------|---------------|
| `gcx stacks list/get/create/update/delete/pause/resume` | Stack management | **Missing** |
| `gcx access-policies` CRUD | Cloud access policy management | **Missing** |
| `gcx credentials` | Bootstrap telemetry credentials | **Missing** |
| `gcx invites` CRUD | Org invite management | **Missing** |
| `gcx organizations` | Org settings | **Missing** |
| `gcx quotas` | Org quota management | **Missing** |
| `gcx rbac` | Custom RBAC roles CRUD | **Missing** |
| `gcx oauth` | OAuth SSO get/update | **Missing** |
| `gcx saml` | SAML SSO get/update | **Missing** |
| `gcx sso-settings` | SSO provider settings | **Missing** |
| `gcx scim` | SCIM resource CRUD | **Missing** |
| `gcx securevalues` | Unified Storage secure values | **K8s tier** (SecureValue, Keeper resources: `gcx resources get securevalues\|keepers`) |
| `gcx cloud-migrations` | Cloud migration CRUD | **Missing** |
| `gcx stack-regions` | List available regions | **Missing** |
| `gcx labels` | GOPS labels CRUD | **Missing** |
| `gcx assistant tunnel/auth/prompt/credentials/agents/rotate` | Assistant/AI management | **Partially exists** (`gcx assistant prompt` with streaming A2A; tunnel/auth/credentials/agents/rotate **missing**) |

---

## 8. IRM (Incident Response)

| Feature | Old CLI | New gcx |
|---------|---------|---------|
| Incidents CRUD | Full (list with --query, --from, --to, --lookback) | Provider: list, get, create, close, open, activity, severities |
| OnCall integrations | Full CRUD | Provider: Full CRUD (exists) |
| OnCall escalation-chains | Full CRUD | Provider: exists |
| OnCall schedules | Full CRUD | Provider: exists |
| OnCall webhooks | Full CRUD | Provider: exists |
| OnCall routes | Full CRUD | Provider: exists |
| OnCall shifts | Full CRUD | Provider: exists |
| OnCall escalation-policies | Full CRUD | Provider: exists |
| OnCall alert-groups | Full CRUD | Provider: exists |
| OnCall alerts | Full | Provider: exists |
| OnCall resolution-notes | Full CRUD | Provider: exists |
| OnCall shift-swaps | Full CRUD | Provider: exists |
| OnCall escalation | Escalate alerts | Provider: exists |
| OnCall organizations | Manage orgs | Provider: exists |
| OnCall user-groups | Manage groups | Provider: exists |
| OnCall slack-channels | Manage Slack | Provider: exists |
| OnCall personal-notification-rules | Manage rules | **Missing** |
| OnCall teams | Manage teams | Provider: exists |
| OnCall users | Manage users | Provider: exists |

**Assessment:** IRM is well-covered in gcx via providers. Minor gaps only.

---

## 9. Synthetics & Testing

| Feature | Old CLI | New gcx |
|---------|---------|---------|
| SM checks CRUD | Full | Provider: Full (exists) |
| SM probes | Full CRUD | Provider: list only |
| SM install-probes | Install command | **Missing** |
| k6 projects | Full CRUD | Provider: exists |
| k6 tests | Full CRUD | Provider: exists |
| k6 schedules | Full CRUD | Provider: exists (`gcx k6 schedules` CRUD) |
| k6 env-vars | Full CRUD | Provider: exists (`gcx k6 env-vars` CRUD) |
| k6 load-zones | Manage zones | Provider: exists (`gcx k6 load-zones` CRUD + `allowed-load-zones\|allowed-projects`) |
| k6 testrun | Run tests (CRD) | Provider: exists (`gcx k6 test-run emit\|status\|runs`) |
| k6 token | Exchange AP token for k6 token | Provider: `auth` command exists (`gcx k6 auth token`) |

**Assessment:** Well-covered. Minor gaps in SM probes CRUD and install-probes.

---

## 10. Knowledge Graph

**Assessment:** Both old and new have comprehensive KG support. The gcx provider exposes data-plane and read operations (status, rules, model-rules, suppressions, relabel-rules, entities, scopes, assertions, search, insights, inspect, health, open); stack onboarding and dataset or display configuration remain in the Grafana UI rather than duplicated in the CLI.

---

## 11. Key Utilities & Features

### Missing Infrastructure

| Feature | Old CLI | New gcx |
|---------|---------|---------|
| Audit logging | Local audit log to `~/.config/gcx/audit/` | **Missing** |
| Self-observability | OTLP tracing/metrics of CLI itself | **Missing** |
| Dry-run with diff | `--dry-run --diff` shows field changes | `--dry-run` exists but no `--diff` |
| Upsert/if-not-exists | `--upsert`, `--if-not-exists` flags | **Missing** |
| CSV output | `--output csv` | **Missing** |
| JSONPath output | `--output jsonpath=<expr>` | **Missing** |
| jq filter | `--jq` flag for post-processing | **Missing** |
| `--open` flag | Open resource in browser | **Missing** on most commands |
| Resource URL generation | Deep-link to Grafana UI | **Missing** |
| Schema/example per resource | `schema`, `example` subcommands on every resource | `resources schemas/examples` (centralized) |
| Manifest-based CRUD | K8s-style YAML with apiVersion/kind | **Exists** (resources system) |
| Interactive prompts | `cmdutil/prompt.go` | **Missing** |
| Time range parsing | `now-7d`, RFC3339, relative | **Exists** in query commands |

### New gcx-Only Features (Advantages)

| Feature | Description |
|---------|-------------|
| K8s-native resource tier | Dynamic client via `k8s.io/client-go` for Grafana 12+ APIs |
| Resource selectors | `kind[.group.version]/name` flexible selection |
| Provider plugin system | Self-registering providers with adapter pattern |
| `dev scaffold/import/generate` | Go code generation from resources |
| `dev lint` | Rego-based linting engine with custom rules |
| `dev serve` | Local dev server with live reload |
| Prometheus labels/metadata/targets | Direct Prometheus API access |
| Loki labels/series | Direct Loki API access |
| Multi-path push/pull | Multiple `-p` paths for resource operations |
| Agent mode | Auto-detected JSON output for LLM tooling |
| Config layers | System/user/local config hierarchy |
| `gcx sigil` | Sigil AI observability provider (conversations search/show) |
| `gcx frontend` | Frontend Observability provider (apps CRUD, sourcemaps) |
| `gcx appo11y` | App Observability settings/overrides management |
| `gcx setup instrumentation` | Instrumentation discovery and apply (status/discover/show/apply) |
| `gcx assistant prompt` | Grafana Assistant via A2A protocol (streaming SSE) |
| `gcx api` | Direct HTTP passthrough to any Grafana API endpoint |
| `gcx commands` | Machine-readable command catalog with token_cost, llm_hint, resource_types |
| `gcx help-tree` | Token-efficient command tree for agents |
| K8s resource types | SecureValue, Connection, Plugin, Repository, Job, Keeper, AnnouncementBanner, SandboxSettings, AlertEnrichment, Query, Preferences, ShortURL |

---

## 12. Agentic Discoverability Gap Analysis

The old CLI has a deeply layered system for helping LLM/agent consumers discover capabilities, understand schemas, and choose next actions -- all without needing to read docs. The new gcx now covers the most critical layers (`commands`, `help-tree`, `token_cost`/`llm_hint` annotations) with remaining gaps in agent-card, explain, and permission annotations.

### What the Old CLI Has (10 Layers of Agentic Metadata)

#### Layer 1: `gcx commands` -- Hierarchical Command Catalog
- Lists every command with rich metadata: `full_path`, `description`, `long`, `example`, `args`, `flags` (with types/defaults), recursive `subcommands`
- Includes **`token_cost`** ("small"/"medium"/"large") so agents can estimate output size before calling
- Includes **`llm_hint`** per command (e.g., `"--since 30m --limit 20"`) -- tells agents how to scope large commands
- Includes **`required_scope`**, **`required_role`**, **`required_action`** -- agents know permissions before calling
- Supports `--flat -o json` for machine parsing
- **gcx status: EXISTS** (`gcx commands --flat -o json`; includes `resource_types` section with operation hints; supports `--validate` for live checks against a Grafana instance)

#### Layer 2: `gcx help-tree` -- Token-Efficient Command Tree
- Compact indented text tree, 2 spaces per level
- Leaf commands show args and flags inline
- Detects enum patterns in flags (e.g., `ignore|fail|abort`)
- Shows `# Tips` and `# hint:` annotations inline
- Supports `--depth` to limit nesting and subtree filtering
- **gcx status: EXISTS** (`gcx help-tree`)

#### Layer 3: `gcx agent-card` -- A2A Agent Card
- Machine-readable JSON describing: name, version, capabilities (dry_run, batch_input, stdin_pipe, json_output), skills (resource groups with actions), authentication schemes (env vars, flags), output modes
- Follows the Agent-to-Agent protocol standard
- Can filter to single skill: `--command dashboards`
- **gcx status: MISSING**

#### Layer 4: `gcx <resource> schema` -- Per-Resource JSON Schema
- Every resource type has a `schema` subcommand outputting full JSON Schema
- Uses Go's `jsonschema` reflection -- always in sync with code
- Schema includes `$defs` with all nested type definitions
- Wrapped in K8s-style manifest envelope (apiVersion, kind, metadata, spec)
- **gcx status: PARTIALLY EXISTS** -- `resources schemas` fetches OpenAPI schemas from server, but:
  - Not per-resource subcommand (centralized only)
  - Provider-backed resources use hand-written schemas in Registration structs
  - No Go type reflection -- schemas are manually maintained JSON blobs

#### Layer 5: `gcx <resource> example` -- Per-Resource Examples
- Every resource type has an `example` subcommand with realistic field values
- Respects `-o` format (json, yaml, text)
- Can include API reference URL
- **gcx status: PARTIALLY EXISTS** -- `resources examples` lists provider examples, but:
  - Not per-resource subcommand (centralized only)
  - Examples are hand-written JSON in provider Registration structs
  - K8s-tier resources don't have examples

#### Layer 6: `gcx explain <resource>[.field.path]` -- Field-Level Docs
- Drills into schema: `gcx explain dashboards.spec.title` shows type + description
- Dereferences `$ref` pointers
- `--recursive` expands all nested objects
- **gcx status: MISSING**

#### Layer 7: Command Annotations (Cobra metadata)
Old CLI annotates every command with structured metadata:

| Annotation | Purpose | gcx Status |
|------------|---------|------------|
| `token_cost` (small/medium/large) | LLM cost estimation before invocation | **MISSING** |
| `llm_hint` | Scoping guidance for large commands | **MISSING** |
| `tips` | Multi-line operational guidance | **MISSING** |
| `required_scope` | Access policy scopes needed | **MISSING** |
| `required_role` | Grafana role needed (Viewer/Editor/Admin) | **MISSING** |
| `required_action` | RBAC action needed | **MISSING** |
| `supports_graph` | Command supports `-o graph` | **MISSING** |

- Enforced via `consistency_test.go`: every executable command MUST have `token_cost`; every "large" command MUST have `llm_hint`
- Permission annotations are centrally managed in `permissions.go` and inherited by child commands

#### Layer 8: Permission-Enriched Error Messages
- On 403 errors, old CLI wraps the error with the command's permission annotations:
  - "Required access policy scope(s): metrics:read"
  - "Hint: add the missing scope(s) to your access policy"
  - "Required Grafana role: Editor"
- Uses `LastCommandAnnotations` global to correlate errors with command metadata
- On unknown commands: `"hint: run 'gcx agent-card -o json' to explore all available commands"`
- **gcx status: PARTIALLY EXISTS** -- `cmd/gcx/fail/` has structured `DetailedError` with `Suggestions` array and `DocsLink`, but no permission-specific enrichment or command annotation correlation

#### Layer 9: `gcx skills install/list` -- Bundled Agent Playbooks
- Ships markdown-based skill files (SKILL.md) with YAML frontmatter
- Installable into different LLM environments: Claude, Cursor, Windsurf, Copilot
- Main skill teaches a 4-step discovery protocol:
  1. Inspect tree (`help-tree`)
  2. Inspect commands (`commands --flat`)
  3. Inspect help (specific command `--help`)
  4. Pull examples/schemas
- **gcx status: MISSING** (skills are installed externally via Claude Code config, not bundled in binary)

#### Layer 10: `gcx api-resources` -- Resource Type Registry
- Lists all known resource types with NAME, APIVERSION, KIND
- Quick scan of available surface area
- **gcx status: PARTIALLY EXISTS** -- `resources schemas` covers this but with more ceremony (requires server connection for K8s resources)

### What New gcx Has That Old CLI Doesn't

| Feature | Description |
|---------|-------------|
| **`--json ?` field discovery** | Agents discover queryable fields without docs: `gcx resources get --json ?` |
| **`--json field1,field2` selection** | Select specific fields from JSON output |
| **Agent mode auto-detection** | Detects CLAUDECODE, CURSOR_AGENT, GITHUB_COPILOT env vars; switches to JSON by default |
| **Structured error JSON** | `DetailedError` with `summary`, `details`, `suggestions[]`, `docsLink`, `exitCode` -- machine-parseable |
| **Partial failure envelope** | `{"items": [...], "error": {...}}` when some operations succeed and others fail |
| **Provider self-registration** | Schemas and examples registered at init time via `adapter.Register()` |
| **Progressive disclosure** | `--json ?` -> field list -> `--json field1,field2` -> filtered output |

### Gap Summary Table

| Capability | Old CLI | New gcx | Gap Severity |
|------------|---------|---------|-------------|
| Command catalog with metadata | `commands` (token_cost, llm_hint, permissions) | **Exists** (`gcx commands --flat -o json` with token_cost, llm_hint, resource_types) | **CLOSED** |
| Token-efficient tree | `help-tree` (depth, subtree, tips) | **Exists** (`gcx help-tree`) | **CLOSED** |
| A2A agent card | `agent-card` (skills, auth, capabilities) | **Missing** | **MEDIUM** |
| Per-resource schema | `<resource> schema` (auto-generated from Go types) | `resources schemas` (centralized, server-fetched) | **LOW** (different approach) |
| Per-resource example | `<resource> example` | `resources examples` (centralized) | **LOW** (different approach) |
| Field-level docs | `explain resource.field.path` | **Missing** | **MEDIUM** |
| Token cost annotation | Every command annotated | **Exists** (visible in `gcx commands` output, e.g. assistant prompt has `token_cost: "large"`) | **CLOSED** |
| LLM hint annotation | Large commands hint scoping args | **Exists** (visible in `gcx commands` output, e.g. `llm_hint` on assistant prompt) | **CLOSED** |
| Permission annotations | required_scope/role/action on commands | **Missing** | **MEDIUM** |
| Permission-enriched errors | 403 -> "add scope X to your policy" | Generic structured errors only | **MEDIUM** |
| Bundled LLM skills | `skills install` for Claude/Cursor/etc | External only (not in binary) | **LOW** |
| Consistency enforcement | Test: all cmds have token_cost, large have llm_hint | **Partially exists** (annotations supported; enforcement test coverage unknown) | **MEDIUM** |
| Field discovery (`--json ?`) | **Missing** | Exists | Old CLI gap |
| Agent mode auto-detect | **Missing** | Exists | Old CLI gap |
| Structured error JSON | Basic error strings | `DetailedError` with suggestions | Old CLI gap |

### Recommended Priority for Agentic Features

**P0 -- Must have for agent usability:**
1. ~~**Command annotations**: `token_cost` + `llm_hint` on all commands~~ -- **DONE** (supported in `gcx commands` output)
2. **Consistency test**: Enforce annotations exist on all commands (prevents drift)
3. ~~**`commands` or `help-tree`**: At least one machine-readable command catalog~~ -- **DONE** (both `gcx commands` and `gcx help-tree` exist)

**P1 -- High value:**
4. **Permission annotations** on commands with error enrichment
5. **`explain`** for field-level schema exploration
6. **`agent-card`** for A2A protocol compliance

**P2 -- Nice to have:**
7. **Bundled skills** (`skills install`)
8. **Tips annotation** on parent commands
9. **`api-resources`** shorthand

---

## Summary: Migration Priority

### P0 -- Critical Gaps (core workflow blockers)
1. **`init` bootstrap flow** -- Users can't onboard without this
2. **Global `--output` / `-o`** -- Consistent output formatting everywhere
3. **`--dry-run --diff`** -- Essential for safe operations
4. **`stacks` management** -- Can't manage stacks at all
5. **`access-policies`** -- Can't manage IAM

### P1 -- High Priority (frequent user workflows)
1. **Alerting**: contact-points, notification-policies, mute-timings, templates
2. **`status`** -- Unified health view (firing alerts + incidents + OnCall)
3. **`diff`** -- Local vs remote comparison
4. ~~**Adaptive metrics/logs/traces**~~ -- **DONE** (`gcx metrics adaptive`, `gcx logs adaptive`, `gcx traces adaptive`)
5. ~~**Standalone `metrics/logs/traces` query aliases**~~ -- **DONE** (`gcx metrics query`, `gcx logs query`, `gcx traces search`, `gcx profiles query`)
6. **`--upsert` / `--if-not-exists`** -- Idempotent operations
7. **`--jq` filter** -- Output post-processing
8. **Datasource CRUD** (create/update/delete)
9. **Folders dedicated CRUD** with folder-specific flags
10. **`auth status`** -- Token health check

### P2 -- Medium Priority (important but less frequent)
1. Platform: `rbac`, `oauth`, `saml`, `sso-settings`, `scim`
2. Resource: `annotations`, `teams`, `users`, `serviceaccounts`, `plugins`
3. Observability: `integrations` (note: `recording-rules` via K8s tier, `connections` via K8s tier, `faro` **done**)
4. `telemetry` analysis suite (cardinality, pipeline verification)
5. `billing metrics`
6. `audit` (cloud-side resource scanning)
7. Session management (temporary scoped sessions)
8. Verb-first shortcuts (`get/create/update/delete`)
9. CSV and JSONPath output formats
10. ~~Shell completion~~ -- **DONE**

### P3 -- Low Priority (nice-to-have)
1. `library-panels`, `public-dashboards`, `reports` (note: `playlists`, `snapshots` available via K8s tier)
2. `correlations`, `silences`
3. `cloud-migrations`, `labels` (note: `securevalues` available via K8s tier)
4. `assistant` tunnel/auth/agents (note: `assistant prompt` **done**)
5. `otlp-endpoint` (note: `app-o11y` **done**, `alloy` partially covered by fleet/setup)
6. `agent-card`, `explain` (note: `commands` and `help-tree` **done**)
7. Self-observability (OTLP tracing)
8. Audit logging
9. `usage get/unused`
10. `cloud-provider` (AWS/Azure integrations)
