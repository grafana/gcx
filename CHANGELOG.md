## v0.2.10 (unreleased)

### Breaking Changes

- **`gcx setup instrumentation` removed** — the entire `gcx setup instrumentation` command subtree (`apply`, `discover`, `show`, `status`) has been removed with no backward-compatibility alias. Use the new `gcx instrumentation` provider commands instead.
- **`InstrumentationConfig` kind removed** — `setup.grafana.app/v1alpha1` / `InstrumentationConfig` manifests are no longer accepted by `gcx resources push`. Decompose them into `Cluster` + `App` manifests for `instrumentation.grafana.app/v1alpha1`.

### Migration: `gcx setup instrumentation` → `gcx instrumentation`

| Old command | New command |
|-------------|-------------|
| `gcx setup instrumentation apply -f config.yaml` | `gcx instrumentation clusters create -f cluster.yaml` + `gcx instrumentation apps create -f app.yaml` |
| `gcx setup instrumentation discover <cluster>` | `gcx instrumentation discover <cluster>` |
| `gcx setup instrumentation show <cluster>` | `gcx instrumentation clusters get <cluster>` (K8s flags only) or `gcx instrumentation status --cluster <cluster>` (full view) |
| `gcx setup instrumentation status` | `gcx instrumentation status` |

**Manifest migration:** Replace `InstrumentationConfig` manifests with separate `Cluster` and `App` resource files:

```yaml
# Old: setup.grafana.app/v1alpha1 / InstrumentationConfig
# New: instrumentation.grafana.app/v1alpha1 / Cluster
apiVersion: instrumentation.grafana.app/v1alpha1
kind: Cluster
metadata:
  name: my-cluster
spec:
  costMetrics: true
  nodeLogs: true
```

```yaml
# New: instrumentation.grafana.app/v1alpha1 / App
apiVersion: instrumentation.grafana.app/v1alpha1
kind: App
metadata:
  name: my-cluster-payments
spec:
  cluster: my-cluster
  namespace: payments
  tracing: true
  logging: true
```

### New Features

- Add `gcx instrumentation` top-level provider with `clusters` and `apps` CRUD commands
- Integrate `Cluster` and `App` kinds with `gcx resources pull` / `gcx resources push` pipeline
- Optimistic-lock protection: writes are rejected if remote contains items absent from local manifest
- `gcx instrumentation clusters setup`: cluster name is now optional — the interactive wizard prompts for it when omitted. The wizard is driven by `charmbracelet/huh`, with line-oriented accessible mode in piped shells. Post-install hint now lists the required access policy token scopes (`metrics:read`, `set:alloy-data-write`) and links to the access-policies docs.
- `gcx instrumentation` commands now emit a structured stderr footer after the primary result, surfacing the eventual-consistency quirk: submitted configs are persisted but won't appear in `list`/`get` until the Alloy collector reports discovery (~30s after Helm install). Coverage spans all 14 instrumentation commands — post-mutation notes on writes (`clusters create/update/delete`, `clusters setup`, `apps create/update/delete`); empty-state notes and next-step hints on zero-row reads (`clusters list`, `apps list`, `status`, `clusters discover`); 404 hints on `clusters get` and `apps get`; FAIL hints on `clusters check`; and a universal "data reflects collector state" note on all successful reads. The footer uses strict `warn:` → `note:` → `hint:` ordering regardless of call order. Hints render as `hint: to <purpose>:` + an indented command line for triple-click copy-paste. Prefixes are plain text, capped at 3 hints per site. This is a provider-local spike; cross-cutting standardization is tracked in #549.

## v0.2.9 (2026-04-23)

- Consolidated `gcx auth` and `gcx config` into a unified `gcx login` command
- Renamed `gcx sigil` command and provider to `gcx aio11y` (AI Observability)
- Fixed `gcx irm` to pass `--max-age` filter through to the OnCall backend
- Added PyPI publishing to the release workflow
- Bumped Claude plugin version automatically on release
- Added Grafana Cloud API tiers architectural overview to the docs
- Added compatibility matrix to the README


## v0.2.8 (2026-04-20)

- Rename `gcx sigil` command and provider to `gcx aio11y` (AI Observability)
- Fix OAuth refresh lockout when running multiple gcx invocations concurrently
- Improve typed API error handling for datasource queries
- Rename OnCall/Incidents references to IRM across docs and CLI
- Default SLO definitions list limit to all results
- Add Homebrew installation support with docs
- Allow login through grafana.com/launch
- Unified CLI UX consistency pass across commands
- Reorganise and clean up README
- Add DatasourceProvider interface and plugin system for datasources
- Add billing subtree and generic series leaf to metrics
- Add --from/--to time range flags to all kg commands
- Validate kg --scope flag values against known scopes
- Remove redundant kg search entities command
- Filter incidents by tags and from/to time range
- Add fleet auth error scopes suggestion
- Add sigil skill to claude-plugin
- Guide agents to use Grafana Assistant for reasoning tasks
- Recognise OPENCODE as an agent mode
- Bump Kubernetes dependencies to v0.35.4 and Docker deps
- Update anthropics/claude-code-action workflow digest

## v0.2.7 (2026-04-15)



- Default `gcx slo definitions list --limit` to 0 (print all SLOs); raise agent `token_cost` to medium with hint to use `--limit` when narrowing output
- Consolidate OnCall + Incidents under unified `irm` provider
- Add adaptive metrics segments and exemptions commands
- Adopt server-side pagination for list commands
- Auto-discover Synthetic Monitoring URL from plugin settings
- Improve skills list output, add installed status, single-skill install
- Fix adaptive telemetry auth when using OAuth for Grafana
- Suggest `stacks:read` scope on cloud stack lookup 403
- Update OAuth coverage warning to remove incidents/oncall
- Align assistant SSE HTTP client timeout with `--timeout` flag
- Fix `gcx dev serve` not exiting on Ctrl+C
- Fix watcher error channel handling
- Trim Knowledge Graph CLI surface and typed resources
- Add marketing bento-box slide with verified CLI commands
- Upgrade ASCII logo to ANSI Shadow font
- Use "k6" instead of "K6" in UI text
- Restructure README for better narrative flow
- Dependency updates (Go modules, GitHub Actions)


## v0.2.6 (2026-04-13)



- Add `--limit` flag with default pagination (50) to all list commands
- Add retry transport for rate limiting and transient HTTP errors
- Unified HTTP client construction with debug logging
- Set consistent User-Agent header on all HTTP clients
- Add `alert instances list` with server-side state filtering
- Route OnCall requests through OAuth proxy
- Add `skills install` command for .agents-compatible harnesses
- Add `--expr` flag alias for datasource query commands
- Add curl-pipe installer script with shell-specific PATH instructions
- Fix config context selection before env overrides in provider loaders
- Fix SLO definitions commands not inheriting parent config loader
- Restore shell tab-completion
- Add Fish shell completion docs
- Update Go and Docker dependencies


## v0.2.5 (2026-04-10)



- Rename `faro` CLI command to `frontend`
- Auto-derive context name from server URL during login
- Add OAuth experimental warning to login flow
- Add `assistant:chat` scope to OAuth flow
- Add HTTP traffic debug logging
- Add Sigil generations, scores, and judge commands
- Add latency and reachability to synth checks status
- Add access property to datasource list and get
- Centralized agent annotations with consistency tests
- Fix null stream labels and missing content in log queries
- Improve human-readable logs query output
- Require `--instant` flag for TraceQL instant metrics
- Fall back to `/api/ds/query` for Loki and Prometheus
- Resolve datasources across all API groups
- Make config edit resilient to broken configs
- Fix invalid CLI commands in docs and skills


## v0.2.4 (2026-04-08)



- Add sigil evaluator/rule CRUD and templates commands
- Add sigil agents and eval read-only commands
- Add synthetic monitoring private probe management
- Restructure adaptive metrics command layout
- Promote `--json ?` as primary discovery for programmatic use
- Reject stray arguments on group commands
- Improve error messages for wrong/unknown commands
- Fix graph output for non-series query results
- Fix empty timestamp display in traces instant tables
- Fix synth check status to use alertSensitivity thresholds
- Include alerting enrichments in SLO definitions get/list
- Add titles to all issues
- Restructure docs into VISION, ARCHITECTURE, DESIGN split
- Fix command syntax and install instructions in README

## v0.2.3 (2026-04-07)



- Fix OAuth token persistence on refresh
- Add styled tables and ASCII logo with Neon Dark theme
- Add assistant investigation CRUD commands
- Improve agent discoverability with progressive disclosure
- Fix error propagation in natural key matching
- Add natural key matching for cross-stack resource push
- Add adaptive log drop-rules CLI and client
- Add datasource autodiscovery
- Update Kubernetes and CI dependencies
- Improve auth login and README documentation


## v0.2.2 (2026-04-03)

- Add Grafana Assistant prompt command (A2A protocol)
- Add Faro (Frontend Observability) provider
- Add Sigil AI observability provider with conversations
- Add Tempo trace query commands (search, get, metrics, tags)
- Lift signal commands to top-level (metrics, logs, traces, profiles)
- Add gcx-observability skill for Claude plugin
- Improve auth login error when server is missing
- Trim trailing slash from server URL in config
- Centralize --json field selection in provider commands
- Remove kg service-dashboard command
- Align datasource query docs with Loki terminology
- Recommend manual token config over auth login in docs


## v0.2.1 (2026-04-02)

- Add automated release process with AI-generated changelogs
- Remove Knowledge Graph (kg) env commands


## v0.2.0 (2026-04-02)

- Add OAuth browser-based login for Grafana (`gcx auth login`)
- Add declarative instrumentation setup (`gcx setup`)
- Add Pyroscope SelectSeries support with time-series and top modes
- Add adaptive logs exemptions & segments CLI
- Add adaptive traces policy CRUD commands
- Rename KG assertions commands to insights
- Fix synthetic monitoring check management UX
- Fix version info for `go install` builds
- Fix stack status DTO handling
- Fix Loki query usage errors
- Remove KG frontend-rules command

## v0.1.0 (2026-03-30)

- Initial release of gcx (formerly grafanactl)
- K8s resource tier: get, push, pull, delete, edit, validate, serve via Grafana K8s API
- Cloud provider tier with pluggable providers: SLO, Synthetic Monitoring, OnCall, Fleet, Knowledge Graph, Incidents, Alerting, App O11y, Adaptive Telemetry
- Datasource queries: Prometheus, Loki, Pyroscope
- Dashboard snapshots via Image Renderer
- Resource linting engine with Rego rules and PromQL/LogQL validators
- Agent mode with command catalog and resource type metadata
- Config system with named contexts, env var overrides, TLS support
- Live dev server with reverse proxy and websocket reload
- Output codecs: JSON, YAML, text, wide, CSV, graph
- CI/CD with conventional commits, golangci-lint, reference doc drift checks
