# AGENTS.md — Agent Entry Point

> Lightweight map for autonomous coding agents. Read this first, then navigate to specific docs on demand.

## Quick Start

**grafanactl** is a kubectl-style CLI for managing Grafana 12+ resources via its Kubernetes-compatible API. Built in Go (~14k LOC), it uses `k8s.io/client-go` and Cobra.

## Documentation Map

### Primary References

| Document | What It Covers | Read When |
|----------|---------------|-----------|
| [CLAUDE.md](CLAUDE.md) | Build commands, test commands, project conventions, code org standards | Running builds/tests, understanding conventions |
| [agent-docs/README.md](agent-docs/README.md) | Full index of architecture docs with navigation guide | Deep-diving into any architectural domain |

### Architecture Docs (in `agent-docs/`)

| Document | Domain | Read When |
|----------|--------|-----------|
| [architecture.md](agent-docs/architecture.md) | System-wide architecture overview | First-time orientation, understanding overall design |
| [patterns.md](agent-docs/patterns.md) | Recurring patterns catalog (10 patterns) | Before implementing new features |
| [resource-model.md](agent-docs/resource-model.md) | Resource, Selector, Filter, Discovery abstractions | Modifying resource handling |
| [cli-layer.md](agent-docs/cli-layer.md) | Command tree, Options pattern, lifecycle | Adding/modifying CLI commands |
| [client-api-layer.md](agent-docs/client-api-layer.md) | Dynamic client, auth, error translation | API communication changes |
| [config-system.md](agent-docs/config-system.md) | Contexts, env vars, TLS, namespace resolution | Config or auth changes |
| [data-flows.md](agent-docs/data-flows.md) | Push/Pull/Serve/Delete pipelines | Modifying resource sync flows |
| [project-structure.md](agent-docs/project-structure.md) | Build system, CI/CD, dependencies, directory layout | Build issues, adding deps |
| `.claude/skills/update-agent-docs/` | Agent-docs maintenance | After significant code changes |

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

## Essential Commands

```bash
make build       # Build to bin/grafanactl
make tests       # Run all tests with race detection
make lint        # Run golangci-lint
make all         # lint + tests + build + docs
make docs        # Generate + build all documentation
```

## Package Map

```
cmd/grafanactl/
├── root/        CLI root (logging, global flags)
├── config/      Config management commands (set, use-context, view...)
├── resources/   Resource commands (get, list, push, pull, serve...)
├── fail/        Structured error → user-friendly message conversion
└── io/          Output codec registry (json, yaml, text, wide)

internal/
├── config/      Config types, loader, editor, rest.Config builder, stack-id discovery
├── resources/
│   ├── *.go     Core types: Resource, Selector, Filter, Descriptor, Resources collection
│   ├── discovery/  API resource discovery, registry index, GVK resolution
│   ├── dynamic/    k8s dynamic client wrapper (namespaced + versioned)
│   ├── local/      FSReader, FSWriter (disk I/O)
│   ├── process/    Processors: ManagerFields, ServerFields, Namespace
│   └── remote/     Pusher, Puller, Deleter, FolderHierarchy, Summary
├── server/      Live dev server (Chi router, reverse proxy, websocket reload)
├── grafana/     OpenAPI client (health checks, version detection)
├── format/      JSON/YAML codecs with format auto-detection
├── httputils/   HTTP helpers (used by serve command's proxy)
├── secrets/     Redactor for config view
└── logs/        slog/klog integration
```
