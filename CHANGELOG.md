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
