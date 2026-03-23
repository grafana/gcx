# AGENTS.md — Agent Entry Point

> Lightweight map for autonomous coding agents. Read this first, then navigate to specific docs on demand.

## Quick Start

**grafanactl** is a kubectl-style CLI for managing Grafana 12+ resources via its Kubernetes-compatible API. Built in Go (~14k LOC), it uses `k8s.io/client-go` and Cobra.

## Documentation Map

### Primary References

| Document | What It Covers | Read When |
|----------|---------------|-----------|
| [CLAUDE.md](CLAUDE.md) | Build commands, test commands, project conventions, code org standards | Running builds/tests, understanding conventions |
| [docs/architecture/README.md](docs/architecture/README.md) | Full index of architecture docs with navigation guide | Deep-diving into any architectural domain |

### Architecture Docs (in `docs/architecture/`)

| Document | Domain | Read When |
|----------|--------|-----------|
| [architecture.md](docs/architecture/architecture.md) | System-wide architecture overview | First-time orientation, understanding overall design |
| [patterns.md](docs/architecture/patterns.md) | Recurring patterns catalog (15 patterns) | Before implementing new features |
| [resource-model.md](docs/architecture/resource-model.md) | Resource, Selector, Filter, Discovery abstractions | Modifying resource handling |
| [cli-layer.md](docs/architecture/cli-layer.md) | Command tree, Options pattern, lifecycle | Adding/modifying CLI commands |
| [client-api-layer.md](docs/architecture/client-api-layer.md) | Dynamic client, auth, error translation | API communication changes |
| [config-system.md](docs/architecture/config-system.md) | Contexts, env vars, TLS, namespace resolution | Config or auth changes |
| [data-flows.md](docs/architecture/data-flows.md) | Push/Pull/Serve/Delete pipelines | Modifying resource sync flows |
| [project-structure.md](docs/architecture/project-structure.md) | Build system, CI/CD, dependencies, directory layout | Build issues, adding deps |

### Reference Guides (in `docs/reference/`)

| Document | Domain | Read When |
|----------|--------|-----------|
| [provider-discovery-guide.md](docs/reference/provider-discovery-guide.md) | Pre-implementation research and design for new providers | Before designing a new provider (discovery phase) |
| [provider-guide.md](docs/reference/provider-guide.md) | Step-by-step guide: implement + register a new provider | Adding a new Grafana product provider |
| [design-guide.md](docs/reference/design-guide.md) | UX requirements: output, exit codes, errors, naming | Before implementing features, reviewing CLI UX |

## Architecture at a Glance

```
CLI Layer (cmd/grafanactl/)          ← Cobra commands, zero business logic
    ↓
Business Logic (internal/resources/) ← Resource model, selectors, filters, processors
    ↓
Client Layer (internal/resources/dynamic/) ← k8s dynamic client wrapper
    ↓
Grafana REST API (/apis endpoint)    ← K8s-compatible API (Grafana 12+)
```

**Core flow**: User input → Selector (partial) → Discovery → Filter (resolved) → Dynamic Client → Grafana API

## Key Conventions

- **Options pattern**: Every command uses `opts struct` + `setup(flags)` + `Validate()` + constructor
- **Processor pipeline**: `Processor.Process(*Resource) error` — composable transformations for push/pull
- **errgroup concurrency**: Bounded parallelism (default 10) for all batch I/O operations
- **Folder-before-dashboard**: Push pipeline does topological sort — folders pushed level-by-level before other resources
- **Config = kubectl kubeconfig**: Named contexts with server/auth/namespace, env var overrides
- **Format-agnostic data fetching**: Commands fetch all data regardless of `--output` format; codecs control display, not data acquisition (see Pattern 13 in `docs/architecture/patterns.md`)
- **PromQL via promql-builder**: Use `github.com/grafana/promql-builder/go/promql` for PromQL construction, not string formatting (see Pattern 14 in `docs/architecture/patterns.md`)

## Essential Commands

```bash
make build       # Build to bin/grafanactl
make tests       # Run all tests with race detection
make lint        # Run golangci-lint
make all         # lint + tests + build + docs
make docs        # Generate + build all documentation
```

> **Before pushing to a PR branch, always run `make all` with agent mode explicitly disabled.**
> The `make docs` step regenerates `docs/reference/cli/` by running the binary, which
> auto-detects agent mode from env vars like `CLAUDECODE` or `CLAUDE_CODE`. When those
> are set, the binary flips default output formats (e.g. `"json"` instead of `"table"`),
> producing wrong docs. `GRAFANACTL_AGENT_MODE=false` overrides all detection:
> ```
> GRAFANACTL_AGENT_MODE=false make all
> ```
> Skipping this causes CI to fail with docs drift.

> **Update `docs/architecture/` when a PR changes architecture.** Specifically: adding
> or removing packages under `internal/` or `cmd/`, introducing new architectural
> patterns, changing core abstractions (Resource, Selector, Filter, Discovery),
> or adding a new provider. Routine bug fixes, test changes, and small features
> do not need it. Follow the structural checks in
> [docs/reference/doc-maintenance.md](docs/reference/doc-maintenance.md) to audit
> for staleness — keeping these docs accurate prevents agents from making bad
> assumptions in future sessions.

## Package Map

```
cmd/grafanactl/
├── root/        CLI root (logging, global flags)
├── config/      Config management commands (set, use-context, view...)
├── resources/   Resource commands (get, schemas, push, pull, delete, edit, validate)
├── dashboards/  Dashboard commands (snapshot via Image Renderer)
├── datasources/ Datasource commands (list, get, prometheus, loki, pyroscope, tempo, generic)
│   └── query/   Query subcommand shared infrastructure (codecs, time parsing, per-kind constructors)
├── providers/   Provider list command
├── api/         Raw API passthrough command (direct Grafana API calls)
├── linter/      Linting commands (run, new, rules, test — mounted under dev lint)
├── dev/         Developer commands (import, scaffold, generate, lint, serve)
├── fail/        Structured error → user-friendly message conversion
└── io/          Output codec registry (json, yaml, text, wide)

internal/
├── config/      Config types, loader, editor, rest.Config builder, stack-id discovery, context name helpers
├── cloud/       GCOM HTTP client for Grafana Cloud stack discovery
├── resources/
│   ├── *.go     Core types: Resource, Selector, Filter, Descriptor, Resources collection
│   ├── adapter/    ResourceAdapter interface, Factory, ResourceClientRouter, self-registration
│   ├── discovery/  API resource discovery, registry index, GVK resolution, OpenAPI schema fetcher
│   ├── dynamic/    k8s dynamic client wrapper (namespaced + versioned)
│   ├── local/      FSReader, FSWriter (disk I/O)
│   ├── process/    Processors: ManagerFields, ServerFields, Namespace
│   └── remote/     Pusher, Puller, Deleter, FolderHierarchy, Summary
├── providers/   Provider plugin system (interface, registry, self-registration)
│   ├── slo/        SLO provider (definitions, reports)
│   ├── synth/      Synthetic Monitoring provider (checks, probes)
│   └── alert/      Alert provider (rules, groups — read-only)
├── dashboards/  Dashboard Image Renderer client (PNG snapshots)
├── query/       Datasource query clients
│   ├── prometheus/  Prometheus HTTP query client
│   └── loki/        Loki HTTP query client
├── agent/       Agent mode detection (IsAgentMode, env-var + flag detection)
├── terminal/    TTY/pipe detection (IsPiped, NoTruncate, Detect) for output suppression
├── linter/      Linting engine (Rego rules, report aggregation, PromQL/LogQL validators)
├── graph/       Terminal chart rendering (ntcharts + lipgloss)
├── testutils/   Shared test utilities
├── server/      Live dev server (Chi router, reverse proxy, websocket reload)
├── grafana/     OpenAPI client (health checks, version detection)
├── format/      JSON/YAML codecs with format auto-detection
├── httputils/   HTTP helpers (used by serve command's proxy)
├── secrets/     Redactor for config view
└── logs/        slog/klog integration
```
