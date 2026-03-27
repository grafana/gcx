# docs/

Documentation index for gcx.

## Directory Structure

```
docs/
├── architecture/     # Per-domain codebase analysis — output of /map-codebase
├── adrs/             # Architecture Decision Records — grouped by research slug
│   └── {research-slug}/  # e.g., adrs/my-research/001-title.md
├── specs/            # Spec packages, design docs, and implementation plans
├── research/         # Findings and analysis — point-in-time snapshots
├── investigations/   # Investigation reports and debugging post-mortems
├── reference/        # Evergreen tool/API docs — updated in place
│   ├── cli/          # Auto-generated CLI reference (make docs)
│   ├── doc-maintenance.md  # Rules for which docs to update when
│   └── stage-checklist.md  # Per-stage completion template
├── guides/           # User-facing how-to guides
├── methodologies/    # Architectural philosophy — evergreen
├── _templates/       # One template per document type
│   ├── adr.md
│   └── research.md
└── _archive/         # Raw output, code dumps, PoC artifacts
```

## Document Types

### `architecture/` — Architecture Analysis

For: per-domain codebase analysis — system architecture, patterns, data flows,
CLI layer, client API, config system, and project structure. Content describes
the current state of the codebase, not decisions. Updated in place as the
codebase evolves. See [architecture/README.md](architecture/README.md) for
the full index.

### `adrs/` — Architecture Decision Records

For: recording an architectural decision — what was decided, why, and what
the consequences are.

**Subdirectory convention**: ADRs are grouped in subdirectories named after the
research report that spawned them. Derive the slug by stripping the date prefix
and `.md` extension from the research filename. ADRs with no research origin go
under `adrs/legacy/`.

**Numbering**: local to each subdirectory (`NNN-title.md`). No global sequence.

**Lifecycle**: `proposed` -> `accepted` -> `deprecated` | `superseded`

**Required header fields** (all 4 must be present):
```
**Created**: YYYY-MM-DD
**Status**: proposed | accepted | deprecated | superseded
**Bead**: gcx-experiments-xxx (or "none")
**Supersedes**: path/to/old.md (or "none")
```

Template: [`_templates/adr.md`](_templates/adr.md)

### `specs/` — Specs and Design Documents

For: implementation plans, design proposals, and structured spec-driven
development (SDD). Each feature lives in its own subdirectory. SDD specs
use the `spec.md` + `plan.md` + `tasks.md` triple; design docs may use
a simpler plan + stage structure.

Use `/plan-spec` to generate SDD specs and `/build-spec` to implement.

### `research/` — Research Reports

For: investigated a topic, evaluated options, gathered findings. No lifecycle —
point-in-time snapshots. Filename must include date prefix.

**Required header fields**:
```
**Created**: YYYY-MM-DD
**Confidence**: X% (Low|Medium|High)
**Sources**: N
```

Template: [`_templates/research.md`](_templates/research.md)

### `investigations/` — Investigation Reports

For: debugging post-mortems, root cause analyses. Point-in-time snapshots.
Filename must include date prefix: `YYYY-MM-DD-short-name.md`.

### `reference/` — Reference Docs

For: documenting how to use a shipped tool or workflow. Evergreen, updated
in place. Includes auto-generated CLI reference in `cli/`.

### `guides/` — User Guides

For: user-facing how-to documentation. Indexed via `guides/index.md`.

### `methodologies/` — Methodologies

Evergreen philosophical and architectural docs. No format changes — content
determines structure.

### `_archive/` — Archive

Raw output, code dumps, PoC artifacts. No format requirements.

## Naming Conventions

| Scope | Convention | Example |
|---|---|---|
| Feature subdirs | Lowercase hyphenated | `my-feature/`, `auth-refactor/` |
| Point-in-time docs | `YYYY-MM-DD-short-name.md` | `2025-11-14-implementation-plan.md` |
| Evergreen docs | Descriptive name only (no date) | `permissions-philosophy.md` |
| Special dirs | Underscore prefix | `_templates/`, `_superseded/`, `_archive/` |

Short names drop the feature prefix — the directory provides context:
`implementation-plan.md` not `my-feature-implementation-plan.md`.
