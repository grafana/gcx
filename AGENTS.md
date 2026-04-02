# AGENTS.md — Agent Entry Point

> Lightweight map for autonomous coding agents. Read this first, then navigate to specific docs on demand.

## Quick Start

**gcx** is a unified CLI for managing Grafana resources. It operates in two tiers: (1) a **K8s resource tier** that uses Grafana 12+'s Kubernetes-compatible API via `k8s.io/client-go` for dashboards, folders, and other K8s-native resources, and (2) a **Cloud provider tier** with pluggable providers for Grafana Cloud products (SLO, Synthetic Monitoring, OnCall, Fleet Management, etc.) that use product-specific REST APIs. Built in Go, it uses Cobra for CLI structure.

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
| [patterns.md](docs/architecture/patterns.md) | Recurring patterns catalog (18 patterns) | Before implementing new features |
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
| [migration-gap-analysis.md](docs/reference/migration-gap-analysis.md) | Gap analysis between grafana-cloud-cli and gcx, with prioritized migration roadmap | Understanding what's missing before planning new features or migrations |

### Templates (in `docs/_templates/`)

Spec and planning templates for structured work. Use these when creating specs in `docs/specs/`.

| Template | Use For |
|----------|---------|
| [feature-spec.md](docs/_templates/feature-spec.md) | New feature specs (problem, requirements, acceptance criteria) |
| [feature-plan.md](docs/_templates/feature-plan.md) | Architecture/design plan for a feature spec |
| [feature-tasks.md](docs/_templates/feature-tasks.md) | Task breakdown with dependency waves |
| [bugfix-spec.md](docs/_templates/bugfix-spec.md) | Bug fix specs (current vs expected behavior, repro steps) |
| [refactor-spec.md](docs/_templates/refactor-spec.md) | Refactoring specs (behavioral contract, migration steps) |
| [adr.md](docs/_templates/adr.md) | Architecture Decision Records |
| [research.md](docs/_templates/research.md) | Research reports |

## Architecture at a Glance

```
CLI Layer (cmd/gcx/)              ← Cobra commands, zero business logic
    ↓
Business Logic (internal/resources/)     ← Resource model, selectors, filters, processors
    ↓                          ↓
K8s Dynamic Client         Provider Adapters (internal/providers/*)
(internal/resources/       ← Pluggable Cloud product providers
 dynamic/)                   (SLO, SM, OnCall, Fleet, KG, Incidents, Alert...)
    ↓                          ↓
Grafana K8s API            Product REST APIs
(/apis endpoint)           (Cloud-specific endpoints)
```

**K8s tier flow**: User input → Selector → Discovery → Filter → Dynamic Client → Grafana API
**Provider tier flow**: User input → Provider CLI → Provider Client → Product REST API

## Key Conventions

- **Options pattern**: Every command uses `opts struct` + `setup(flags)` + `Validate()` + constructor
- **Processor pipeline**: `Processor.Process(*Resource) error` — composable transformations for push/pull
- **errgroup concurrency**: Bounded parallelism (default 10) for all batch I/O operations
- **Folder-before-dashboard**: Push pipeline does topological sort — folders pushed level-by-level before other resources
- **Config = kubectl kubeconfig**: Named contexts with server/auth/namespace, env var overrides
- **Format-agnostic data fetching**: Commands fetch all data regardless of `--output` format; codecs control display, not data acquisition (see Pattern 13 in `docs/architecture/patterns.md`)
- **PromQL via promql-builder**: Use `github.com/grafana/promql-builder/go/promql` for PromQL construction, not string formatting (see Pattern 14 in `docs/architecture/patterns.md`)

## Essential Commands

> **Without devbox**: All `make` targets require `devbox`. If you don't have it, use the direct Go commands instead:
> ```bash
> go build -buildvcs=false -o bin/gcx ./cmd/gcx/   # replaces make build
> go test ./...                                      # replaces make tests
> ```
> Always build to `bin/gcx` (not a temp binary) so the binary stays at a stable path for testing.

```bash
make build       # Build to bin/gcx
make tests       # Run all tests with race detection
make lint        # Run golangci-lint
make all         # lint + tests + build + docs
make docs        # Generate + build all documentation
```

> **Before pushing to a PR branch, always run `make all` with agent mode explicitly disabled.**
> The `make docs` step regenerates `docs/reference/cli/` by running the binary, which
> auto-detects agent mode from env vars like `CLAUDECODE` or `CLAUDE_CODE`. When those
> are set, the binary flips default output formats (e.g. `"json"` instead of `"table"`),
> producing wrong docs. `GCX_AGENT_MODE=false` overrides all detection:
> ```
> GCX_AGENT_MODE=false make all
> ```
> Skipping this causes CI to fail with docs drift.

> **Doc maintenance is a gate before creating a PR or finishing a session.**
> If code changes touch `internal/` or `cmd/` structure, new architectural patterns,
> core abstractions (Resource, Selector, Filter, Discovery), or add a provider:
> run the structural checks in [docs/reference/doc-maintenance.md](docs/reference/doc-maintenance.md)
> and update `CLAUDE.md` (package map), `DESIGN.md` (package table), and relevant
> `docs/architecture/` files. Routine bug fixes, test changes, and small features
> do not need it. Keeping these docs accurate prevents agents from making bad
> assumptions in future sessions.

## Package Map

```
cmd/gcx/
├── root/        CLI root (logging, global flags)
├── auth/        OAuth login command (browser-based PKCE flow)
├── config/      Config management commands (set, use-context, view...)
├── resources/   Resource commands (get, schemas, push, pull, delete, edit, validate)
├── dashboards/  Dashboard commands (snapshot via Image Renderer)
├── datasources/ Datasource commands (list, get, query)
│   └── query/   Auto-detecting query command (GenericCmd only; shared infra in internal/datasources/query/)
├── providers/   Provider list command
├── api/         Raw API passthrough command (direct Grafana API calls)
├── linter/      Linting commands (run, new, rules, test — mounted under dev lint)
├── commands/    Commands catalog (agent-consumable metadata, resource types, live validation)
├── helptree/    Compact text tree for agent context injection (help-tree command)
├── setup/       Setup command area (onboarding, instrumentation — not a provider)
│   └── instrumentation/  Instrumentation subcommands (status, discover, show, apply)
├── dev/         Developer commands (import, scaffold, generate, lint, serve)
└── fail/        Structured error → user-friendly message conversion

internal/
├── auth/        OAuth PKCE flow, token refresh transport
├── config/      Config types, loader, editor, rest.Config builder, stack-id discovery, context name helpers
├── cloud/       GCOM HTTP client for Grafana Cloud stack discovery
├── fleet/       Shared fleet base client (HTTP, auth, config — used by fleet provider and setup/instrumentation)
├── setup/
│   └── instrumentation/  Manifest types, instrumentation client, optimistic lock comparison
├── resources/
│   ├── *.go     Core types: Resource, Selector, Filter, Descriptor, Resources collection
│   ├── adapter/    ResourceAdapter interface, Factory, ResourceClientRouter, self-registration, slug-ID helpers
│   ├── discovery/  API resource discovery, registry index, GVK resolution, OpenAPI schema fetcher
│   ├── dynamic/    k8s dynamic client wrapper (namespaced + versioned)
│   ├── local/      FSReader, FSWriter (disk I/O)
│   ├── process/    Processors: ManagerFields, ServerFields, Namespace
│   └── remote/     Pusher, Puller, Deleter, FolderHierarchy, Summary
├── providers/   Provider plugin system (interface, registry, self-registration)
│   ├── alert/      Alert provider (rules, groups — read-only)
│   ├── fleet/      Fleet Management provider (pipeline and collector resources)
│   ├── incidents/  IRM Incidents provider
│   ├── k6/         K6 Cloud provider (projects, tests, runs, envvars)
│   ├── kg/         Knowledge Graph (Asserts) provider
│   ├── logs/       Logs signal provider (Loki queries + Adaptive Logs commands)
│   ├── metrics/    Metrics signal provider (Prometheus queries + Adaptive Metrics commands)
│   ├── oncall/     OnCall provider (schedules, integrations, escalation chains)
│   ├── appo11y/    App Observability provider (overrides, settings — singleton resources)
│   ├── profiles/   Profiles signal provider (Pyroscope queries + adaptive stub)
│   ├── slo/        SLO provider (definitions, reports)
│   ├── synth/      Synthetic Monitoring provider (checks, probes)
│   ├── traces/     Traces signal provider (Tempo queries + Adaptive Traces commands)
│   └── adaptive/   Shared adaptive subpackages (metrics/, logs/, traces/, auth/) — imported by signal providers
├── dashboards/  Dashboard Image Renderer client (PNG snapshots)
├── datasources/ Datasource HTTP client (legacy REST API)
│   └── query/   Shared query CLI utils (time parsing, codecs, opts, resolve helpers — used by signal providers and GenericCmd)
├── query/       Datasource query clients
│   ├── prometheus/  Prometheus HTTP query client
│   └── loki/        Loki HTTP query client
├── agent/       Agent mode detection, command annotations, known-resource registry with operation hints
├── terminal/    TTY/pipe detection (IsPiped, NoTruncate, Detect) for output suppression
├── linter/      Linting engine (Rego rules, report aggregation, PromQL/LogQL validators)
├── graph/       Terminal chart rendering (ntcharts + lipgloss)
├── testutils/   Shared test utilities
├── server/      Live dev server (Chi router, reverse proxy, websocket reload)
├── grafana/     OpenAPI client (health checks, version detection)
├── output/      Output codec registry (json, yaml, text, wide — field selection, formatting)
├── format/      JSON/YAML codecs with format auto-detection
├── httputils/   HTTP helpers (used by serve command's proxy)
├── secrets/     Redactor for config view
└── logs/        slog/klog integration
```

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:ca08a54f -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Both `bd` and `dolt` commands MUST be executed from the repository root, not from worktrees
- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd dolt push
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->
