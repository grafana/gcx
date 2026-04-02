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
