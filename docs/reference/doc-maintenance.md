# Documentation Maintenance Rules

## Which Docs to Update for Which Changes

### Adding/Changing a Package

| Document | Update Required? |
|----------|-----------------|
| `DESIGN.md` | Yes — update package map table |
| `docs/architecture/` | Yes — run structural checks below |
| `CLAUDE.md` | Yes — update package map section |
| `README.md` | Only if it affects CLI usage or quick start |

### Changing a Core Feature or API

| Document | Update Required? |
|----------|-----------------|
| `DESIGN.md` | Yes — if architectural decisions changed |
| `README.md` | Yes — if user-visible behavior changed |
| `docs/architecture/` | Yes — run structural checks below |
| `docs/reference/cli/` | Automatic — `make docs` regenerates CLI reference |

### Adding a New ADR

| Document | Update Required? |
|----------|-----------------|
| `DESIGN.md` | Yes — add row to ADR summary table |
| New file | Create `docs/adrs/<research-slug>/NNN-title.md` |

### Adding a New Provider

| Document | Update Required? |
|----------|-----------------|
| `DESIGN.md` | Yes — add to package map |
| `CLAUDE.md` | Yes — update package map section |
| `docs/architecture/` | Yes — run structural checks below |
| `docs/reference/provider-guide.md` | Only if the pattern changes |

### Changing Beads Workflow or Conventions

| Document | Update Required? |
|----------|-----------------|
| `CONTRIBUTING.md` | Yes — issue tracking section |
| `CLAUDE.md` | Only if core rules change |

### Changing CLI Flags or Interface

| Document | Update Required? |
|----------|-----------------|
| `README.md` | Yes — update CLI flags or usage section |
| `docs/reference/cli/` | Automatic — `make docs` regenerates |
| `docs/reference/design-guide.md` | If UX conventions changed |

## General Rules

1. **Every PR should include doc updates** for any user-visible or
   architecture-level change.
2. **DESIGN.md is the architecture index** — if you add a new doc file, link
   it from DESIGN.md.
3. **CLAUDE.md is the agent entry point** — keep it as a TOC; put
   details in docs/.
4. **Don't duplicate** — cross-link between docs instead of copying content.
5. **docs/ is the system of record** — organize by content type, not audience.
6. **Run `make docs`** after any CLI changes — regenerates reference docs.

---

## Architecture Docs Structural Checks

When a PR changes `internal/` or `cmd/` structure, run these checks against
`docs/architecture/` to detect staleness. Only flag **structural changes**
that shift architecture — not line-level edits, test changes, or formatting.

### 1. Package Inventory

**Check:** `ls internal/`

Every top-level directory in `internal/` should appear in:
- `docs/architecture/architecture.md` (layered architecture description)
- `docs/architecture/project-structure.md` (directory layout section)
- `DESIGN.md` (package map table)

**Severity:** Missing coverage (medium) for new architectural layers. Low for
utility packages nested under existing layers.

### 2. Command Inventory

**Check:** `ls cmd/gcx/*/`

Every command group directory in `cmd/gcx/` should appear in:
- `docs/architecture/cli-layer.md` (command tree section)
- `CLAUDE.md` (package map)

**Severity:** Missing coverage (high) for user-facing command groups.

### 3. Pattern Count

**Check:** Count patterns documented in `docs/architecture/patterns.md`.
Cross-reference against code for new patterns:
- Provider interface pattern (if new provider packages exist)
- Translation adapter pattern (if `adapter.go` files exist in provider packages)
- Any new interface with 3+ implementations

**Severity:** Missing coverage (medium) for patterns used across multiple packages.

### 4. Config Model

**Check:** Read `internal/config/types.go`

The `GrafanaConfig` and `Context` struct shapes should match the data model
described in `docs/architecture/config-system.md`. Flag:
- New struct fields representing new concepts
- New nested structs
- New environment variable constants

**Severity:** Stale reference (high) if data model diagram is materially wrong.

### 5. Pipeline Count

**Check:** Count distinct data flow pipelines:
- Push pipeline (local -> Grafana via k8s API)
- Pull pipeline (Grafana -> local via k8s API)
- Delete pipeline (local -> Grafana deletion)
- Serve pipeline (local -> browser preview)
- Provider-specific pipelines (push/pull via REST API)

Each should be documented in `docs/architecture/data-flows.md`.

**Severity:** Missing coverage (high) for entirely new pipeline types.

### 6. README Index

**Check:** `ls docs/architecture/*.md`

Every `.md` file in `docs/architecture/` (except README.md) must be listed
in `docs/architecture/README.md`'s navigation table.

**Severity:** Structural issue (medium) for unlisted docs.

### Update Guidelines

When updating docs to fix violations:

1. **Preserve existing style** — match formatting, heading levels, writing style
2. **Add, don't rewrite** — insert new sections or rows; don't reorganize unaffected content
3. **Stay high-level** — architecture-level descriptions, not implementation details
4. **Cross-link** — add corresponding entries in README.md and CLAUDE.md if appropriate
5. **Include confidence** — new pattern entries in `patterns.md` should include a confidence %
6. **Update metadata** — change the `Last updated` date in `docs/architecture/README.md`

### Severity Definitions

| Severity | Definition | Action |
|----------|-----------|--------|
| **Stale reference** | Documented path/type/package no longer exists or materially changed | Must fix |
| **Missing coverage** | New package/command/pattern/pipeline undocumented | Should fix |
| **Structural issue** | README index incomplete, metadata outdated | Nice to fix |
