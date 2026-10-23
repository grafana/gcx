# Documentation

## For Users

- **[Installation](sources/installation.md)** — Install gcx via Homebrew or binary download
- **[Configuration](sources/configuration.md)** — Set up contexts, authentication, and environments
- **[Guides](sources/guides/)** — How-to guides for common workflows
- **[CLI Reference](sources/cli-reference.md)** — Generated release reference

## Local preview

To build the Grafana.com-style docs locally:

1. Change to the `docs/` directory.
2. Run `make docs`.
3. Open `http://localhost:3002/docs/grafana/next/as-code/observability-as-code/grafana-cli/gcx/`.

## For Contributors & Agents

- **[CLAUDE.md](../CLAUDE.md)** — Agent entry point: doc map and build commands
- **[CONSTITUTION.md](../CONSTITUTION.md)** — Project invariants and constraints (authoritative)
- **[ARCHITECTURE.md](../ARCHITECTURE.md)** — Architecture overview and pipeline diagrams
- **[DESIGN.md](../DESIGN.md)** — CLI UX design: command grammar, output model, taste rules
- **[CONTRIBUTING.md](../CONTRIBUTING.md)** — Dev setup, testing, contribution workflow
- **[Engineering RFCs](rfcs/README.md)** — Proposals, tradeoffs, and open questions
- **[Architecture](architecture/README.md)** — Deep-dive architecture docs per domain

## Maintaining documentation

Follow the [contribution guide](../CONTRIBUTING.md#contributing-code) for when an [RFC](rfcs/README.md) is required. Developers choose their own implementation and planning tools. Keep design decisions and terminology in the RFC; keep current user-facing behavior in the relevant guide.

See [documentation maintenance](reference/doc-maintenance.md) for what to update when behavior or architecture changes. Browse the directories for available documents instead of maintaining duplicate inventories.
