# gcx — Agent & Developer Entry Point

> Lightweight map for autonomous coding agents. Read this first, then navigate to specific docs on demand.

## Quick Start

**gcx** is a unified CLI for managing Grafana resources. It operates in two tiers: (1) a **K8s resource tier** that uses Grafana 12+'s Kubernetes-compatible API via `k8s.io/client-go` for dashboards, folders, and other K8s-native resources, and (2) a **Cloud provider tier** with pluggable providers for Grafana Cloud products (SLO, Synthetic Monitoring, OnCall, Fleet Management, etc.) that use product-specific REST APIs. Built in Go, it uses Cobra for CLI structure.

## Documentation Map

| File | Purpose |
|------|---------|
| [VISION.md](VISION.md) | Goals, product surface, roadmap themes, release timeline |
| [CONSTITUTION.md](CONSTITUTION.md) | Invariants — things that cannot change without explicit human approval |
| [ARCHITECTURE.md](ARCHITECTURE.md) | System overview (all 7 subsystems), pipeline diagrams, ADR index |
| [DESIGN.md](DESIGN.md) | CLI UX design: command grammar, output model, exit codes |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Dev setup, testing environment, contribution workflow |
| [docs/architecture/](docs/architecture/) | Deep-dive architecture docs (patterns, resource model, CLI layer, data flows, …) |
| [docs/design/](docs/design/) | Prescriptive UX implementation rules (output, errors, agent mode, naming, …) |
| [docs/reference/](docs/reference/) | Provider guides, CLI reference, migration analysis |
| [docs/_templates/](docs/_templates/) | Spec and planning templates (feature, bugfix, refactor, ADR, research) |

## Architecture at a Glance

Two tiers: **K8s resource tier** (dashboards, folders via `/apis`) and **Cloud provider tier** (SLO, SM, OnCall, etc. via product REST APIs). See [ARCHITECTURE.md](ARCHITECTURE.md) for pipeline diagrams and extension pipelines.

## Key Conventions

> Authoritative source: [CONSTITUTION.md](CONSTITUTION.md) (invariants) and [DESIGN.md](DESIGN.md) (UX rules). This is the quick-reference summary.

- **Options pattern**: Every command uses `opts struct` + `setup(flags)` + `Validate()` + constructor
- **Processor pipeline**: `Processor.Process(*Resource) error` — composable transformations for push/pull
- **errgroup concurrency**: Bounded parallelism (default 10) for all batch I/O operations
- **Folder-before-dashboard**: Push pipeline does topological sort — folders pushed level-by-level before other resources
- **Config = kubectl kubeconfig**: Named contexts with server/auth/namespace, env var overrides
- **Format-agnostic data fetching**: Commands fetch all data regardless of `--output` format; codecs control display, not data acquisition (see Pattern 13 in `docs/architecture/patterns.md`)
- **PromQL via promql-builder**: Use `github.com/grafana/promql-builder/go/promql` for PromQL construction, not string formatting (see Pattern 14 in `docs/architecture/patterns.md`)
- **Portable agent skills live under `claude-plugin/skills/`**: Treat that tree as the canonical portable Agent Skills bundle. Do not add distributable gcx skills under repo-local `.agents/skills/` — that changes repo-context discovery semantics for tools that scan `.agents`.

## Essential Commands

```bash
make build       # Build to bin/gcx
make tests       # Run all tests with race detection
make lint        # Run golangci-lint
make all         # lint + tests + build + docs
make docs        # Generate + build all documentation
```

**Without devbox**: replace `make` targets with direct Go commands — `go build -buildvcs=false -o bin/gcx ./cmd/gcx/` and `go test ./...`. Always build to `bin/gcx`.

> **Agent environments**: always prefix with `GCX_AGENT_MODE=false` — agent-mode auto-detection changes output defaults in `make docs`, producing wrong CLI reference docs.

## Testing

```bash
go test ./internal/providers/traces/...   # Run one package
go test -run TestQueryCodec ./internal/... # Run matching tests across packages
go test -race -count=1 ./...              # Full suite with race detection (same as make tests)
```

Prefer table-driven tests. See existing `_test.go` files for patterns.

## Package Map

> Full map with sub-packages: [docs/architecture/project-structure.md](docs/architecture/project-structure.md)

```
cmd/gcx/
  root/         CLI root (logging, global flags)
  auth/         OAuth login (browser PKCE)
  config/       Config management (set, use-context, view)
  resources/    Resource commands (get, schemas, push, pull, delete, edit, validate)
  dashboards/   Dashboard snapshot (Image Renderer)
  datasources/  Datasource commands (list, get, query)
  providers/    Provider list command
  assistant/    Assistant commands (AI-powered investigations)
  api/          Raw API passthrough
  linter/       Linting (mounted under dev lint)
  commands/     Commands catalog (agent metadata)
  helptree/     Help tree for agent context
  setup/        Onboarding + instrumentation
  dev/          Developer tools (import, scaffold, generate, lint, serve)
  fail/         Structured error conversion

internal/
├── auth/        OAuth PKCE flow, token refresh transport
│   └── adaptive/  Shared adaptive telemetry auth (GCOM caching, Basic auth — used by signal providers)
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
│   ├── faro/       Frontend Observability provider (apps CRUD, sourcemaps sub-resource) — CLI: `gcx frontend`
│   ├── fleet/      Fleet Management provider (pipeline and collector resources)
│   ├── incidents/  IRM Incidents provider
│   ├── k6/         K6 Cloud provider (projects, tests, runs, envvars)
│   ├── kg/         Knowledge Graph (Asserts) provider
│   ├── logs/       Logs signal provider (Loki queries + Adaptive Logs commands)
│   ├── metrics/    Metrics signal provider (Prometheus queries + Adaptive Metrics commands)
│   ├── oncall/     OnCall provider (schedules, integrations, escalation chains)
│   ├── appo11y/    App Observability provider (overrides, settings — singleton resources)
│   ├── profiles/   Profiles signal provider (Pyroscope queries + adaptive stub)
│   ├── sigil/      Sigil AI observability provider (conversations, agents, generations, evaluators, rules, templates, scores, judge — via grafana-sigil-app plugin API)
│   ├── slo/        SLO provider (definitions, reports)
│   ├── synth/      Synthetic Monitoring provider (checks, probes)
│   └── traces/     Traces signal provider (Tempo queries + Adaptive Traces commands)
├── dashboards/  Dashboard Image Renderer client (PNG snapshots)
├── datasources/ Datasource HTTP client (legacy REST API)
│   └── query/   Shared query CLI utils (time parsing, codecs, opts, resolve helpers — used by signal providers and GenericCmd)
├── query/       Datasource query clients
│   ├── prometheus/  Prometheus HTTP query client
│   └── loki/        Loki HTTP query client
├── assistant/   Assistant client (A2A streaming, prompt, state management)
│   ├── assistanthttp/  Base HTTP client for grafana-assistant-app plugin API
│   └── investigations/ Investigation CRUD commands, table codecs, API client
├── agent/       Agent mode detection, command annotations, known-resource registry with operation hints
├── style/       Terminal styling (Grafana Neon Dark theme, TableBuilder, ASCII banner, glamour help)
├── terminal/    TTY/pipe detection (IsPiped, NoTruncate, Detect) for output suppression
├── linter/      Linting engine (Rego rules, report aggregation, PromQL/LogQL validators)
├── graph/       Terminal chart rendering (ntcharts + lipgloss)
├── testutils/   Shared test utilities
├── server/      Live dev server (Chi router, reverse proxy, websocket reload)
├── grafana/     OpenAPI client (health checks, version detection)
├── output/      Output codec registry (json, yaml, text, wide — field selection, discovery, k8s unstructured handling)
├── format/      JSON/YAML codecs with format auto-detection
├── retry/       Retry transport (429, 502/503/504, transient connection errors — wraps all HTTP tiers)
├── httputils/   HTTP helpers (used by serve command's proxy)
├── secrets/     Redactor for config view
└── logs/        slog/klog integration
```

## What to Read Before You Start

| Task | Read first | Then |
|------|-----------|------|
| **Adding a new command** | [DESIGN.md](DESIGN.md) (grammar, output model) | [docs/design/](docs/design/) for implementation rules, [ARCHITECTURE.md](ARCHITECTURE.md) § CLI layer |
| **Adding a new provider** | [ARCHITECTURE.md](ARCHITECTURE.md) § Provider System | [docs/reference/provider-guide.md](docs/reference/provider-guide.md), [docs/design/provider-checklist.md](docs/design/provider-checklist.md) |
| **Adding a signal provider command** | [ARCHITECTURE.md](ARCHITECTURE.md) § Signal Providers | Existing signal provider code for the SharedOpts pattern |
| **Modifying resource handling** | [ARCHITECTURE.md](ARCHITECTURE.md) § Resources Pipeline | [docs/architecture/resource-model.md](docs/architecture/resource-model.md), [docs/architecture/data-flows.md](docs/architecture/data-flows.md) |
| **Changing config or auth** | [ARCHITECTURE.md](ARCHITECTURE.md) § Configuration + § Auth | [docs/architecture/config-system.md](docs/architecture/config-system.md), [docs/architecture/client-api-layer.md](docs/architecture/client-api-layer.md) |
| **Fixing a bug** | [ARCHITECTURE.md](ARCHITECTURE.md) for the relevant subsystem | Jump directly to the deep-dive doc for that domain |
| **Planning a new feature** | [VISION.md](VISION.md) (does it belong?), [CONSTITUTION.md](CONSTITUTION.md) (can we build it within the rules?) | [DESIGN.md](DESIGN.md) for UX, [ARCHITECTURE.md](ARCHITECTURE.md) for structure |
| **Reviewing a PR** | [Compliance Hierarchy](#compliance-hierarchy) below | Check all 4 levels in order |

## Compliance Hierarchy

Check work against these docs during planning, design, and implementation — in order of strictness.

| # | Doc | Strictness | What to check | If violated |
|---|-----|-----------|---------------|-------------|
| 1 | [CONSTITUTION.md](CONSTITUTION.md) | **Hard invariant** — violation is a bug | Architecture invariants, dependency rules, provider registration, CLI grammar, typed resource requirements | Stop. Fix before proceeding. Violation requires explicit human approval to waive. |
| 2 | [VISION.md](VISION.md) | **Strategic alignment** — violation is wasted work | Does this belong in gcx? Does it align with dual-purpose design, core beliefs, product surface? | Pause. Confirm direction with a human before investing more effort. |
| 3 | [DESIGN.md](DESIGN.md) | **UX rules** — violation is a UX defect | Output model, exit codes, safety patterns, taste rules in [docs/design/](docs/design/) | Fix. New code must comply. |
| 4 | [ARCHITECTURE.md](ARCHITECTURE.md) | **Structural guidance** — violation is tech debt | Pipeline placement, package boundaries, patterns in [docs/architecture/](docs/architecture/README.md) | Prefer compliance. Deviation is acceptable with rationale (document in commit or ADR). |

**When to check:**
- **Planning/design**: Check VISION (2) and CONSTITUTION (1) — are we building the right thing, and can we build it within the rules?
- **Implementation**: Check DESIGN (3) and ARCHITECTURE (4) — does the code follow UX rules and structural patterns?
- **Pre-flight** (below): Final sweep across all four before pushing.

## Releasing

Automated via `make tag`. Requires `claude` CLI and [`svu`](https://github.com/caarlos0/svu).

```bash
make tag BUMP=patch   # or minor, major
```

This generates a changelog entry (via Claude), updates `CHANGELOG.md` and `.release-notes.md`, commits, tags, and pushes. The tag push triggers the GoReleaser workflow.

**With branch protection** (can't push directly to main): the script will fail at the push step. Instead:
1. Create a branch, commit the changelog, open a PR
2. Merge the PR
3. Tag the merge commit on main and push the tag:
   ```bash
   git checkout main && git pull
   git tag v0.X.Y
   git push origin v0.X.Y
   ```

## Pre-Flight Checklist

Run when code has been modified, before pushing or creating a PR.

1. **Compliance check** — verify changes against the [compliance hierarchy](#compliance-hierarchy) above. CONSTITUTION and DESIGN violations must be fixed. VISION misalignment must be flagged. ARCHITECTURE deviations must be documented.
2. **Sync with base branch**
   ```bash
   git fetch origin main && git rebase origin/main
   ```
3. **Quality gates pass** — `make docs` auto-detects agent mode from env vars (`CLAUDECODE`, `CLAUDE_CODE`) and flips output defaults, producing wrong docs. Always override:
   ```bash
   GCX_AGENT_MODE=false make all
   ```
4. **Doc maintenance gate** — run the structural checks in [docs/reference/doc-maintenance.md](docs/reference/doc-maintenance.md). Update `CLAUDE.md` (package map), `ARCHITECTURE.md` (ADR table), and relevant `docs/architecture/` files if any are stale.
5. **Push**
   ```bash
   git push
   git status   # must show "up to date with origin"
   ```
   Work is not done until push succeeds. If it fails, resolve and retry.
6. **Beads** (if in use) — close completed issues and sync:
   ```bash
   bd close <id>      # from repo root, not worktrees
   bd dolt push
   ```

## Mandatory Pre-Commit Checklist

Run this checklist **before every commit** (not only before PR/push):

1. **Format touched files**
   ```bash
   gofmt -w <touched-go-files>
   ```
2. **Lint passes**
   ```bash
   make lint
   ```
   If `devbox` is unavailable, use the project-local binary/toolchain equivalent.
3. **Targeted tests pass** for changed packages
   ```bash
   go test ./path/to/changed/package/...
   ```
4. **Full test suite passes**
   ```bash
   go test ./...
   ```
5. **Reference docs regenerated when command/output surface changes**
   ```bash
   GCX_AGENT_MODE=false make reference
   ```
6. **No unstaged surprises**
   ```bash
   git status
   ```

## Beads Issue Tracker (optional)

This project can use **bd (beads)** for issue tracking. Run `bd prime` for full command reference.

```bash
bd ready                  # Find available work
bd show <id>              # View issue details
bd update <id> --claim    # Claim work
bd close <id>             # Complete work
bd dolt push              # Sync to Dolt remote (run from repo root, not worktrees)
```
