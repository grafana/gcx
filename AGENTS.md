# gcx — Agent & Developer Entry Point

> Lightweight map for autonomous coding agents. Read this first, then navigate to specific docs on demand.

## Quick Start

**gcx** is a unified CLI for managing Grafana resources. It operates in two tiers: (1) a **K8s resource tier** that uses Grafana 12+'s Kubernetes-compatible API via `k8s.io/client-go` for dashboards, folders, and other K8s-native resources, and (2) a **Cloud provider tier** with pluggable providers for Grafana Cloud products (SLO, Synthetic Monitoring, IRM, Fleet Management, etc.) that use product-specific REST APIs. Built in Go, it uses Cobra for CLI structure.

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

Two tiers: **K8s resource tier** (dashboards, folders via `/apis`) and **Cloud provider tier** (SLO, SM, IRM, etc. via product REST APIs). See [ARCHITECTURE.md](ARCHITECTURE.md) for pipeline diagrams and extension pipelines.

## Key Conventions

> Authoritative source: [CONSTITUTION.md](CONSTITUTION.md) (invariants) and [DESIGN.md](DESIGN.md) (UX rules). This is the quick-reference summary.

- **Options pattern**: Every command uses `opts struct` + `setup(flags)` + `Validate()` + constructor
- **Processor pipeline**: `Processor.Process(*Resource) error` — composable transformations for push/pull
- **errgroup concurrency**: Bounded parallelism (default 10) for all batch I/O operations
- **Folder-before-dashboard**: Push pipeline does topological sort — folders pushed level-by-level before other resources
- **Config = kubectl kubeconfig**: Named contexts with server/auth/namespace, env var overrides
- **Format-agnostic data fetching**: Commands fetch all data regardless of `--output` format; codecs control display, not data acquisition (see Pattern 13 in `docs/architecture/patterns.md`)
- **PromQL via promql-builder**: Use `github.com/grafana/promql-builder/go/promql` for PromQL construction, not string formatting (see Pattern 14 in `docs/architecture/patterns.md`)
- **Datasource query reuse**: Datasource clients that call Grafana's unified datasource query API (`/apis/query.grafana.app/.../query`, with `/api/ds/query` fallback) should reuse `internal/query/grafanaquery` for HTTP transport and `internal/query/dataframe` for Grafana data frame wire types. Do not duplicate POST/fallback/response-limit logic or `GrafanaQueryResponse`/`DataFrame` structs in each datasource package.
- **Agent skill placement follows its audience**: Portable workflows for people using gcx live under `claude-plugin/skills/`; repository-only contributor workflows live under `.claude/skills/`. Do not add distributable gcx skills under repo-local `.agents/skills/` — that changes repo-context discovery semantics for tools that scan `.agents`. Both skill trees are gated: `TestSkillsGcxInvocationsMatchCommandTree` (`cmd/gcx/root/skillsdrift_test.go`) validates every `gcx` invocation in `claude-plugin/skills/` **and** repo-local `.claude/skills/` against the real command tree, failing CI on unknown commands or flags, and `mise run validate-skills` parses the front matter of both.

## Essential Commands

```bash
mise run build       # Build to bin/gcx
mise run tests       # Run all tests with race detection
mise run lint        # Run golangci-lint
mise run gate        # lint + tests + build (no docs) — fast pre-push gate for code changes
mise run all         # lint + tests + build + docs
mise run docs        # Generate + build all documentation
```

**Without mise**: replace with direct Go commands — `go build -buildvcs=false -o bin/gcx ./cmd/gcx/` and `go test ./...`. Always build to `bin/gcx`. Lint runs in Go **module mode** (`golangci-lint`'s `modules-download-mode: readonly`), so no `vendor/` directory is needed locally — the module cache (`go mod download`, run automatically on worktree entry) is sufficient.

> **Agent environments**: always prefix `mise run docs`, `mise run reference`, and `mise run all` with `GCX_AGENT_MODE=false` — agent-mode auto-detection changes output defaults, producing wrong CLI reference docs. The `tests` tasks pin `GCX_AGENT_MODE=false` themselves, so `mise run tests` needs no prefix.

## Testing

Prefer table-driven tests. See existing `_test.go` files for patterns.

## Package Map

> Full map with sub-packages: [docs/architecture/project-structure.md](docs/architecture/project-structure.md)

```
cmd/gcx/
  root/         CLI root (logging, global flags)
  login/        Unified login command (token + OAuth PKCE, interactive prompts)
  config/       Config management (set, use-context, view, check)
  resources/    Resource commands (get, list-types, list-examples, push, pull, delete, edit, validate)
  datasources/  Datasource commands (list, get, query, per-type subcommands via DatasourceProvider)
  providers/    Provider list command
  cloud/        Cloud platform command group (mounts gcx cloud stacks)
  api/          Raw API passthrough
  linter/       Linting (mounted under dev lint)
  commands/     Commands catalog (agent metadata)
  helptree/     Help tree for agent context
  setup/        Onboarding (gcx setup status)
  instrumentation/  Instrumentation Hub commands (clusters, services, setup wizard, status, check, explain, list-explanations)
  skills/       Portable Agent Skills installer for .agents-compatible tools (install/update/list/get/uninstall; get reads bundled SKILL.md or references without installing)
  dev/          Developer tools (import, scaffold, generate, lint, serve)
  fail/         Structured error conversion

internal/        Non-public packages — full annotated map: docs/architecture/project-structure.md
```

## What to Read Before You Start

| Task | Read first | Then |
|------|-----------|------|
| **Adding a new command** | [docs/design/command-naming.md](docs/design/command-naming.md) (verb + placement), [DESIGN.md](DESIGN.md) (grammar, output model) | [docs/design/](docs/design/) for implementation rules, [ARCHITECTURE.md](ARCHITECTURE.md) § CLI layer |
| **Adding a new provider** | [ARCHITECTURE.md](ARCHITECTURE.md) § Provider System | [docs/reference/provider-guide.md](docs/reference/provider-guide.md), [docs/design/provider-checklist.md](docs/design/provider-checklist.md) |
| **Adding a signal provider command** | [ARCHITECTURE.md](ARCHITECTURE.md) § Signal Providers | Existing signal provider code for the SharedOpts pattern |
| **Modifying resource handling** | [ARCHITECTURE.md](ARCHITECTURE.md) § Resources Pipeline | [docs/architecture/resource-model.md](docs/architecture/resource-model.md), [docs/architecture/data-flows.md](docs/architecture/data-flows.md) |
| **Changing config or auth** | [ARCHITECTURE.md](ARCHITECTURE.md) § Configuration + § Auth | [docs/architecture/config-system.md](docs/architecture/config-system.md), [docs/architecture/client-api-layer.md](docs/architecture/client-api-layer.md) |
| **Fixing a bug** | [ARCHITECTURE.md](ARCHITECTURE.md) for the relevant subsystem | Jump directly to the deep-dive doc for that domain |
| **Planning a new feature** | [VISION.md](VISION.md) (does it belong?), [CONSTITUTION.md](CONSTITUTION.md) (can we build it within the rules?) | [DESIGN.md](DESIGN.md) for UX, [ARCHITECTURE.md](ARCHITECTURE.md) for structure |
| **Adding, extending, or reviewing a gcx capability (domain teams)** | Read the repo-local [`integrate-with-gcx`](.claude/skills/integrate-with-gcx/SKILL.md) contributor skill first — it covers necessity, placement, readiness, contract, and self-review | [docs/design/command-naming.md](docs/design/command-naming.md), [docs/reference/provider-guide.md](docs/reference/provider-guide.md) |
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

Release steps live in the `release` skill ([.claude/skills/release/SKILL.md](.claude/skills/release/SKILL.md)) — invoke it when tagging a release.

## Mandatory Pull Request Checklist

You MUST run this checklist when creating a PR or updating an existing PR with new work (addressing PR reviews or fixing bugs). This is distinct from the Mandatory Pre-Commit Checklist below — `mise run all` in step 3 subsumes the individual pre-commit steps; do not substitute the pre-commit checklist here.

1. **Compliance check** — verify changes against the [compliance hierarchy](#compliance-hierarchy) above. CONSTITUTION and DESIGN violations must be fixed. VISION misalignment must be flagged. ARCHITECTURE deviations must be documented.
2. **Sync with base branch**
   ```bash
   git fetch origin main && git rebase origin/main
   ```
3. **Quality gates pass** — `mise run docs` auto-detects agent mode from env vars (`CLAUDECODE`, `CLAUDE_CODE`) and flips output defaults, producing wrong docs. Always override:
   ```bash
   GCX_AGENT_MODE=false mise run all
   ```
4. **Doc maintenance gate** — run the structural checks in [docs/reference/doc-maintenance.md](docs/reference/doc-maintenance.md). Update `CLAUDE.md`, `ARCHITECTURE.md` (ADR table), and relevant `docs/architecture/` files (including the package map in `project-structure.md`) if any are stale.
5. **Push**
   ```bash
   git push
   git status   # must show "up to date with origin"
   ```
   Work is not done until push succeeds. If it fails, resolve and retry.

## Mandatory Pre-Commit Checklist

Run this checklist **before every commit** (not only before PR/push):

1. **Format touched files**
   ```bash
   gofmt -w <touched-go-files>
   ```
2. **Lint passes**
   ```bash
   mise run lint
   ```
3. **Targeted tests pass** for changed packages
   ```bash
   go test ./path/to/changed/package/...
   ```
4. **Full test suite passes**
   ```bash
   go test ./...
   ```
5. **Reference docs regenerated** (CI runs `mise run reference-drift` which fails on any drift)
   ```bash
   GCX_AGENT_MODE=false mise run reference
   ```
   This regenerates CLI reference, env-var reference, config reference, and linter-rules reference. Required when changes touch commands, flags, config fields, env vars, or linter rules.
6. **Docs build succeeds** (CI runs `mise run docs` after the drift check)
   ```bash
   mise run docs
   ```
   If `mise`/`mkdocs` is unavailable, skip — CI will catch build failures.
7. **No unstaged surprises**
   ```bash
   git status
   ```

## GitHub Issues

When creating or commenting on GitHub issues, **always anonymize system-specific details**. Replace real values with placeholders:

- Stack names / context names → `<my-context>`, `<stack>`
- URLs with stack or region identifiers → `https://example-<region>.grafana.net`
- Hosted IDs, stack IDs, org IDs → `12345`, `99999`
- Datasource names with stack slugs → `grafanacloud-<stack>-prom`
- API tokens, credentials → never include, even partially

This applies to issue bodies, comments, and code snippets embedded in issues.
