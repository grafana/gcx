# Design: grafanactl

## Vision

kubectl-style CLI for managing Grafana 12+ resources via its Kubernetes-compatible API.
Built in Go (~14k LOC), uses `k8s.io/client-go` and Cobra. Enables managing dashboards,
folders, alert rules, SLOs, synthetic monitoring checks, and datasource queries from
a single tool with multi-environment context support.

## Pipeline

```
CLI Layer (cmd/grafanactl/)          -- Cobra commands, zero business logic
    |
    v
Business Logic (internal/resources/) -- Resource model, selectors, filters, processors
    |
    v
Client Layer (internal/resources/dynamic/) -- k8s dynamic client wrapper
    |
    v
Grafana REST API (/apis endpoint)    -- K8s-compatible API (Grafana 12+)
```

**Core flow:** User input --> Selector (partial) --> Discovery --> Filter (resolved) --> Dynamic Client --> Grafana API

### Extension Pipelines

```
Provider System (internal/providers/)     -- SLO, Synth, Alert providers
    |                                        (non-K8s Grafana APIs)
    v
Grafana REST API (/api endpoint)          -- Product-specific REST endpoints

Query Layer (internal/query/)             -- Prometheus, Loki, Pyroscope, Tempo
    |                                        (direct HTTP, no k8s machinery)
    v
Datasource HTTP APIs                      -- PromQL, LogQL, profile, trace queries
```

## Architecture Decision Records

| ADR | Title | Status |
|-----|-------|--------|
| [001](docs/adrs/legacy/001-query-under-datasources.md) | Move query under datasources with per-kind subcommands | accepted |

See [docs/research/](docs/research/) for design rationale and [docs/adrs/](docs/adrs/) for all ADRs.

## Package Map

| Package | Responsibility |
|---------|---------------|
| `cmd/grafanactl/root/` | CLI root (logging, global flags) |
| `cmd/grafanactl/config/` | Config management commands |
| `cmd/grafanactl/resources/` | Resource CRUD commands (get, push, pull, delete, edit, validate) |
| `cmd/grafanactl/dashboards/` | Dashboard snapshot command |
| `cmd/grafanactl/datasources/` | Datasource commands (list, get, query subcommands) |
| `cmd/grafanactl/providers/` | Provider list command |
| `cmd/grafanactl/api/` | Raw API passthrough command |
| `cmd/grafanactl/linter/` | Linting commands (run, new, rules, test) |
| `cmd/grafanactl/dev/` | Developer commands (import, scaffold, generate, lint, serve) |
| `cmd/grafanactl/fail/` | Structured error to user-friendly message conversion |
| `cmd/grafanactl/io/` | Output codec registry (json, yaml, text, wide) |
| `internal/config/` | Config types, loader, editor, rest.Config builder |
| `internal/resources/` | Core types: Resource, Selector, Filter, Descriptor |
| `internal/resources/adapter/` | ResourceAdapter interface, Factory, ResourceClientRouter |
| `internal/resources/discovery/` | API resource discovery, registry, GVK resolution |
| `internal/resources/dynamic/` | k8s dynamic client wrapper |
| `internal/resources/local/` | FSReader, FSWriter (disk I/O) |
| `internal/resources/process/` | Processors: ManagerFields, ServerFields, Namespace |
| `internal/resources/remote/` | Pusher, Puller, Deleter, FolderHierarchy, Summary |
| `internal/providers/` | Provider plugin system (interface, registry) |
| `internal/providers/slo/` | SLO provider (definitions, reports) |
| `internal/providers/synth/` | Synthetic Monitoring provider (checks, probes) |
| `internal/providers/alert/` | Alert provider (rules, groups — read-only) |
| `internal/dashboards/` | Dashboard Image Renderer client |
| `internal/query/prometheus/` | Prometheus HTTP query client |
| `internal/query/loki/` | Loki HTTP query client |
| `internal/agent/` | Agent mode detection |
| `internal/terminal/` | TTY/pipe detection |
| `internal/linter/` | Linting engine (Rego rules, PromQL/LogQL validators) |
| `internal/graph/` | Terminal chart rendering |
| `internal/server/` | Live dev server (Chi router, reverse proxy, websocket reload) |
| `internal/grafana/` | OpenAPI client (health checks, version detection) |
| `internal/format/` | JSON/YAML codecs |

## Detailed Architecture Documentation

The `docs/architecture/` directory contains comprehensive architecture analysis:

- [docs/architecture/architecture.md](docs/architecture/architecture.md) — Full system architecture
- [docs/architecture/patterns.md](docs/architecture/patterns.md) — 15 recurring patterns
- [docs/architecture/resource-model.md](docs/architecture/resource-model.md) — Resource, Selector, Filter, Discovery
- [docs/architecture/cli-layer.md](docs/architecture/cli-layer.md) — Command tree, Options pattern
- [docs/architecture/client-api-layer.md](docs/architecture/client-api-layer.md) — Dynamic client, auth
- [docs/architecture/config-system.md](docs/architecture/config-system.md) — Contexts, env vars, TLS
- [docs/architecture/data-flows.md](docs/architecture/data-flows.md) — Push/Pull/Serve/Delete pipelines
- [docs/architecture/project-structure.md](docs/architecture/project-structure.md) — Build system, CI/CD
- [docs/reference/provider-guide.md](docs/reference/provider-guide.md) — How to add a new provider
- [docs/reference/design-guide.md](docs/reference/design-guide.md) — UX requirements

## Reference Documentation

- [docs/README.md](docs/README.md) — Full documentation index
- [CONSTITUTION.md](CONSTITUTION.md) — Project invariants and constraints
- [CONTRIBUTING.md](CONTRIBUTING.md) — Development setup and workflow
