# gcx — Agent & Developer Entry Point

> Lightweight map for autonomous coding agents. Read this first, then navigate to specific docs on demand.

## Quick Start

**gcx** is a unified CLI for managing Grafana resources. It operates in two tiers: (1) a **K8s resource tier** that uses Grafana 12+'s Kubernetes-compatible API via `k8s.io/client-go` for dashboards, folders, and other K8s-native resources, and (2) a **Cloud provider tier** with pluggable providers for Grafana Cloud products (SLO, Synthetic Monitoring, OnCall, Fleet Management, etc.) that use product-specific REST APIs. Built in Go, it uses Cobra for CLI structure.

## Documentation Map

| File | Purpose |
|------|---------|
| [VISION.md](VISION.md) | Goals, product surface, roadmap themes, release timeline |
| [CONSTITUTION.md](CONSTITUTION.md) | Invariants — things that cannot change without explicit human approval |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Architecture overview, pipeline diagrams, ADR index |
| [DESIGN.md](DESIGN.md) | CLI UX design: command grammar, output model, exit codes, taste rules |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Dev setup, testing environment, contribution workflow |
| [docs/architecture/](docs/architecture/) | Deep-dive architecture docs (patterns, resource model, CLI layer, data flows, …) |
| [docs/reference/](docs/reference/) | Provider guides, design implementation guide, CLI reference, migration analysis |
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
  auth/         OAuth PKCE flow, token refresh (adaptive/ for signal providers)
  config/       Config types, loader, editor, rest.Config builder
  cloud/        GCOM HTTP client (stack discovery)
  fleet/        Shared fleet base client
  setup/        Instrumentation manifest types + client
  resources/    Core types + adapter/, discovery/, dynamic/, local/, process/, remote/
  providers/    Provider registry + per-product adapters (alert, faro, fleet, incidents, k6, kg,
                logs, metrics, oncall, appo11y, profiles, sigil, slo, synth, traces)
  dashboards/   Image Renderer client
  datasources/  Datasource HTTP client + query/ shared CLI utils
  query/        Query clients (prometheus/, loki/)
  assistant/    Assistant client (A2A streaming, prompt, investigations)
  agent/        Agent mode detection, command annotations
  style/        Terminal styling (Neon Dark theme, TableBuilder, glamour help)
  terminal/     TTY/pipe detection
  linter/       Rego rules, report aggregation
  graph/        Terminal chart rendering
  output/       Output codec registry (json, yaml, text, wide)
  format/       JSON/YAML codecs, auto-detection
  testutils/    Shared test utilities
  server/       Live dev server
  grafana/      OpenAPI client
  httputils/    HTTP helpers
  secrets/      Redactor for config view
  logs/         slog/klog integration
```

## Compliance Hierarchy

Check work against these docs during planning, design, and implementation — in order of strictness.

| # | Doc | Strictness | What to check | If violated |
|---|-----|-----------|---------------|-------------|
| 1 | [CONSTITUTION.md](CONSTITUTION.md) | **Hard invariant** — violation is a bug | Architecture invariants, dependency rules, provider registration, CLI grammar, typed resource requirements | Stop. Fix before proceeding. Violation requires explicit human approval to waive. |
| 2 | [VISION.md](VISION.md) | **Strategic alignment** — violation is wasted work | Does this belong in gcx? Does it align with dual-purpose design, core beliefs, product surface? | Pause. Confirm direction with a human before investing more effort. |
| 3 | [DESIGN.md](DESIGN.md) | **UX rules** — violation is a UX defect | Output model, exit codes, safety patterns, taste rules, `[CURRENT]`/`[ADOPT]` items in [docs/design/](docs/design/) | Fix. New code must comply with all `[CURRENT]` and `[ADOPT]` items. |
| 4 | [ARCHITECTURE.md](ARCHITECTURE.md) | **Structural guidance** — violation is tech debt | Pipeline placement, package boundaries, patterns in [docs/architecture/](docs/architecture/README.md) | Prefer compliance. Deviation is acceptable with rationale (document in commit or ADR). |

**When to check:**
- **Planning/design**: Check VISION (2) and CONSTITUTION (1) — are we building the right thing, and can we build it within the rules?
- **Implementation**: Check DESIGN (3) and ARCHITECTURE (4) — does the code follow UX rules and structural patterns?
- **Pre-flight** (below): Final sweep across all four before pushing.

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

## Beads Issue Tracker (optional)

This project can use **bd (beads)** for issue tracking. Run `bd prime` for full command reference.

```bash
bd ready                  # Find available work
bd show <id>              # View issue details
bd update <id> --claim    # Claim work
bd close <id>             # Complete work
bd dolt push              # Sync to Dolt remote (run from repo root, not worktrees)
```
