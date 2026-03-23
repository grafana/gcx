# Constitution: grafanactl

> These are invariants. Violating them requires explicit human approval.

## Project Identity

**What it is:** kubectl-style CLI for managing Grafana 12+ resources via its Kubernetes-compatible API.

**Primary values:** correctness, API stability, clean layered architecture, minimal dependencies

## Architecture Invariants

- **Strict layer separation:** `cmd/` contains only CLI wiring (Cobra commands, flag parsing,
  output formatting) — no business logic. All domain logic lives in `internal/`.
- **Unstructured resource model:** Resources are `unstructured.Unstructured` objects — no
  pre-generated Go types. Dynamic discovery at runtime, not compile-time.
- **Folder-before-dashboard ordering:** Push pipeline does topological sort — folders are
  pushed level-by-level before any other resources.
- **Config follows kubeconfig pattern:** Named contexts with server/auth/namespace. Environment
  variable overrides follow the same precedence rules as kubectl.
- **Processor pipeline is composable:** Resource transformations use the `Processor` interface
  (`Process(*Resource) error`). Processors compose into ordered slices at defined pipeline points.
- **Format-agnostic data fetching:** Commands fetch all data regardless of `--output` format;
  codecs control display, not data acquisition.

## Dependency Rules

- `cmd/` may import `internal/`; `internal/` may not import `cmd/`.
- No circular dependencies between packages.
- Provider implementations (`internal/providers/*/`) may import core resource types but not
  other providers.
- Query clients (`internal/query/*/`) bypass the k8s dynamic client — they call datasource
  HTTP APIs directly.
- PromQL construction uses `github.com/grafana/promql-builder/go/promql`, not string formatting.

## Taste Rules

- **Options pattern for every command:** opts struct + `setup(flags)` + `Validate()` + constructor.
- **Table-driven tests:** All Go tests follow [Go wiki conventions](https://go.dev/wiki/TableDrivenTests).
- **Commit format:** Title (one-liner) / What (description) / Why (rationale).
- **Error messages:** Lowercase, no trailing punctuation.
- **errgroup concurrency:** Bounded parallelism (default 10) for all batch I/O operations.

## Quality Standards

- All non-trivial functions have unit tests.
- `make all` (lint + tests + build + docs) must pass before merging.
- `GRAFANACTL_AGENT_MODE=false make all` when running from agent environments
  (prevents agent-mode detection from altering doc generation).
- No linter suppressions without a comment explaining why.
- CI must pass before merging.
- **Architecture docs must stay current with code changes.** When adding or
  removing packages under `internal/` or `cmd/`, introducing new patterns,
  changing core abstractions, or adding a provider — update `docs/architecture/`
  using the structural checks in
  [docs/reference/doc-maintenance.md](docs/reference/doc-maintenance.md).
