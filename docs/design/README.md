# Design Documentation

See `DESIGN.md` at the repository root for the design philosophy overview and navigation.

The docs in this directory are prescriptive implementation guides. Each subsection is tagged with a status marker:

- **`[CURRENT]`** — Implemented and enforced. Follow exactly.
- **`[ADOPT]`** — Not consistently applied yet. New code MUST follow this.
- **`[ASSESS]`** — Not yet implemented. Documented for future direction. ([ThoughtWorks Radar](https://www.thoughtworks.com/radar))

New commands and providers **must comply with all `[CURRENT]` and `[ADOPT]` items**.

| Document | Domain |
|----------|--------|
| [output.md](output.md) | Output codecs, status messages, JSON field selection, mutation summaries |
| [exit-codes.md](exit-codes.md) | Exit code taxonomy and implementation |
| [safety.md](safety.md) | Confirmation prompts, --yes, dry-run, idempotency |
| [errors.md](errors.md) | DetailedError structure, converters, in-band JSON errors |
| [pipe-awareness.md](pipe-awareness.md) | TTY detection, --no-color, pipe behavior |
| [agent-mode.md](agent-mode.md) | Agent mode detection, behavior changes, opt-out |
| [provider-checklist.md](provider-checklist.md) | Provider UX compliance (architecture patterns in [patterns.md](../architecture/patterns.md)) |
| [help-text.md](help-text.md) | Command descriptions, examples format |
| [naming.md](naming.md) | Resource kinds, file naming, config keys, flags |
| [environment-variables.md](environment-variables.md) | Canonical environment variable reference |
