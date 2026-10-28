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
- **[VISION.md](../VISION.md)** — Product fit: goals and core beliefs
- **[ARCHITECTURE.md](../ARCHITECTURE.md)** — Architecture overview and rules
- **[DESIGN.md](../DESIGN.md)** — CLI and agent-facing rules: command grammar, output contract
- **[CONTRIBUTING.md](../CONTRIBUTING.md)** — Contribution workflow, code conventions, dev setup
- **[Contributor guides](contributing/)** — Documentation maintenance and other contributor guides
- **[Engineering RFCs](rfcs/README.md)** — Proposals, tradeoffs, and open questions
- **[Architecture](architecture/README.md)** — Deep-dive architecture docs per domain

## Maintaining documentation

Follow the [contribution guide](../CONTRIBUTING.md#contributing-code) for when an [RFC](rfcs/README.md) is required. Developers choose their own implementation and planning tools. Keep design decisions and terminology in the RFC; keep current user-facing behavior in the relevant guide.

See [documentation maintenance](contributing/doc-maintenance.md) for what to update when behavior or architecture changes. Browse the directories for available documents instead of maintaining duplicate inventories.
