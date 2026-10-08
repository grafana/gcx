# Documentation

## For Users

- **[Installation](sources/installation.md)** — Install gcx via Homebrew or binary download
- **[Configuration](sources/configuration.md)** — Set up contexts, authentication, and environments
- **[Guides](guides/index.md)** — How-to guides for common workflows
- **[CLI Reference](reference/cli/)** — Auto-generated command reference

## Local preview

To build the Grafana.com-style docs locally:

1. Change to the `docs/` directory.
2. Run `make docs`.
3. Open `http://localhost:3002/docs/grafana/next/as-code/observability-as-code/grafana-cli/gcx/`.

## For Contributors & Agents

- **[CLAUDE.md](../CLAUDE.md)** — Agent entry point: doc map, build commands, package index
- **[CONSTITUTION.md](../CONSTITUTION.md)** — Project invariants and constraints (authoritative)
- **[ARCHITECTURE.md](../ARCHITECTURE.md)** — Architecture overview, pipeline diagrams, ADR index
- **[DESIGN.md](../DESIGN.md)** — CLI UX design: command grammar, output model, taste rules
- **[CONTRIBUTING.md](../CONTRIBUTING.md)** — Dev setup, testing, contribution workflow
- **[Engineering RFCs](rfcs/README.md)** — Proposals, tradeoffs, and validation criteria
- **[Architecture](architecture/README.md)** — Deep-dive architecture docs per domain

## Directory Layout

```
docs/
├── architecture/     # Per-domain codebase analysis
├── rfcs/             # Numbered engineering proposals and review workflows
├── adrs/             # Architecture Decision Records
├── sources/          # Grafana.com-mounted user-facing docs
├── reference/        # Evergreen tool/API docs, auto-generated CLI reference
├── guides/           # User-facing how-to guides
├── research/         # Point-in-time research reports
├── specs/            # Ephemeral spec packages (cleaned after merge)
├── _templates/       # Templates for ADRs and research reports
└── assets/           # Images and static assets
```

### Templates

Available in [`_templates/`](_templates/):

| Template | Use For |
|----------|---------|
| `adr.md` | Architecture Decision Records |
| `research.md` | Research reports |

Feature, bugfix and refactor plans are OpenSpec changes under
[`openspec/changes/`](../openspec/), one per PR. Work with enough scope to need its own
design starts with an [RFC](rfcs/README.md); most PRs don't need one.

### Conventions

| Scope | Convention | Example |
|-------|-----------|---------|
| Point-in-time docs | `YYYY-MM-DD-short-name.md` | `2026-03-27-gap-analysis.md` |
| RFCs | `NNN-title.md`, indexed in `rfcs/README.md` | `001-alerting-provider-refactor.md` |
| Evergreen docs | Descriptive name, no date | `provider-guide.md` |
| Feature subdirs | Lowercase hyphenated | `cloud-rest-config/` |

See [reference/doc-maintenance.md](reference/doc-maintenance.md) for which docs to update when.
