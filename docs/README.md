# Documentation

## For Users

- **[Installation](installation.md)** — Install gcx via Homebrew or binary download
- **[Configuration](configuration.md)** — Set up contexts, authentication, and environments
- **[Guides](guides/index.md)** — How-to guides for common workflows
- **[CLI Reference](reference/cli/)** — Auto-generated command reference

Full docs site: [grafana.github.io/gcx](https://grafana.github.io/gcx/)

## For Contributors & Agents

- **[CLAUDE.md](../CLAUDE.md)** — Agent entry point with doc map, conventions, and package index
- **[DESIGN.md](../DESIGN.md)** — Architecture overview, ADR index, and package map
- **[CONSTITUTION.md](../CONSTITUTION.md)** — Project invariants and constraints
- **[Architecture](architecture/README.md)** — Deep-dive architecture docs per domain

## Directory Layout

```
docs/
├── architecture/     # Per-domain codebase analysis (8 docs)
├── adrs/             # Architecture Decision Records (10 ADRs)
├── reference/        # Evergreen tool/API docs, auto-generated CLI reference
├── guides/           # User-facing how-to guides
├── research/         # Point-in-time research reports
├── specs/            # Ephemeral spec packages (cleaned after merge)
├── _templates/       # Templates for ADRs, specs, research reports
└── assets/           # Images and static assets
```

### Templates

Available in [`_templates/`](_templates/):

| Template | Use For |
|----------|---------|
| `adr.md` | Architecture Decision Records |
| `research.md` | Research reports |
| `feature-spec.md` | New feature specs |
| `feature-plan.md` | Architecture/design plans |
| `feature-tasks.md` | Task breakdown with dependency waves |
| `bugfix-spec.md` | Bug fix specs |
| `refactor-spec.md` | Refactoring specs |

### Conventions

| Scope | Convention | Example |
|-------|-----------|---------|
| Point-in-time docs | `YYYY-MM-DD-short-name.md` | `2026-03-27-gap-analysis.md` |
| Evergreen docs | Descriptive name, no date | `provider-guide.md` |
| Feature subdirs | Lowercase hyphenated | `cloud-rest-config/` |

See [reference/doc-maintenance.md](reference/doc-maintenance.md) for which docs to update when.
