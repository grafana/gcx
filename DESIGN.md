# Design: gcx

> Shared vocabulary for how gcx commands look and feel — for developers and agents implementing new commands or providers.
>
> This file covers philosophy and intent. Prescriptive implementation rules are in [docs/reference/design-guide.md](docs/reference/design-guide.md).
> Enforceable invariants (things that cannot change without explicit human approval) are in [CONSTITUTION.md](CONSTITUTION.md).

## Philosophy

gcx is a dual-purpose tool. Every command serves both human operators and AI agents running in pipelines. We optimize for:

- **Predictability** — consistent command grammar, consistent output shapes, consistent error format
- **Composability** — shell-pipeable, machine-parseable by default in agent mode, stable exit codes
- **Transparency** — errors tell you what failed and suggest how to fix it; warnings are explicit, not silent

## CLI Grammar

Command structure follows `$AREA $NOUN $VERB`:

```
gcx slo definitions list
gcx resources push ./dashboards/
gcx logs query --from=1h
gcx oncall schedules get my-schedule
```

Rules (authoritative in [CONSTITUTION.md § CLI Grammar](CONSTITUTION.md#cli-grammar)):

- **Positional arguments** = the subject (resource selectors, UIDs, file paths, expressions)
- **Flags** = modifiers (output format, concurrency, dry-run, filters)
- **Extension commands** nest under their resource type — never at top level
- **`$AREA $VERB`** is valid for tooling commands (`dev`, `config`) where there's no meaningful noun
- If it can be done with list/get/push/pull/delete, it is **not** an extension

## Dual-Purpose Design

Every command works identically for humans and agents. Agent mode changes defaults, not behavior.

| Aspect | Human mode | Agent mode |
|--------|-----------|------------|
| Default output | `text` (table) | `json` |
| Colors | On (TTY) | Off |
| Truncation | On (TTY) | Off |
| Prompts | Interactive | Auto-approved |

Agent mode is active when `GCX_AGENT_MODE=true`, or auto-detected from env vars (`CLAUDECODE`, `CLAUDE_CODE`).
Explicit flags always override: `--output json` works in human mode; `--output text` works in agent mode.

See [docs/reference/design-guide.md § Agent Mode](docs/reference/design-guide.md#6-agent-mode) for detection logic and opt-out.

## Output Model

**STDOUT** = the result. **STDERR** = diagnostics.

- Resource data and operation summaries → stdout
- Progress feedback, warnings, detailed error messages → stderr
- All output goes through the codec system — no unstructured prose as primary output
- Data fetching is **format-agnostic**: commands fetch all available data; codecs control presentation

Default formats by command type:

| Command type | Default | Rationale |
|-------------|---------|-----------|
| `list`, `get` | `text` (table) | Human-scannable |
| `config view` | `yaml` | Config is YAML-native |
| `push`, `pull`, `delete` | Status messages | Operations, not data |
| Agent mode | `json` | Machine-parseable |

The `--json field1,field2` flag selects specific fields. `--json ?` discovers available field paths.

## Exit Codes

| Code | Meaning | When |
|------|---------|------|
| 0 | Success | Command completed without errors |
| 1 | General error | Unexpected error, business logic failure |
| 2 | Usage error | Bad flags, invalid selectors, missing args |
| 3 | Auth failure | 401/403, missing or invalid credentials |
| 4 | Partial failure | Some resources succeeded, others failed |
| 5 | Cancelled | Ctrl+C, `context.Canceled` |
| 6 | Version incompatible | Grafana < 12 detected |

## Safety Patterns

- **Idempotent by default**: `push` is create-or-update. Safe to run repeatedly.
- **Dry-run available**: `push` and `delete` accept `--dry-run`.
- **Prompt before destructive ops**: `delete` prompts unless `--yes`/`-y` or `GCX_AUTO_APPROVE`.
- **No prompt for reversible ops**: push, pull, config changes do not prompt.

## Taste Rules

Code taste rules (options pattern, error messages, test style, commit format) live in [ARCHITECTURE.md § Taste Rules](ARCHITECTURE.md#taste-rules). Authoritative source: [CONSTITUTION.md § Taste Rules](CONSTITUTION.md#taste-rules).

## Implementation Reference

[docs/reference/design-guide.md](docs/reference/design-guide.md) is the prescriptive implementation guide. It contains:

- Output codec setup, status messages (`cmdio.Success`/`Warning`/`Error`/`Info`), JSON field selection
- Exit code implementation with `DetailedError` and converters
- Confirmation (`--yes`), dry-run, idempotency patterns with code examples
- Error formatting, naming conventions, help text standards
- Agent mode detection, behavior switches, opt-out mechanisms
- Table formatting, wide codecs, column design

Status markers (`[CURRENT]`, `[ADOPT]`, `[PLANNED]`) tell you what's enforced vs. aspirational.
New commands and providers **must comply with all `[CURRENT]` and `[ADOPT]` items**.
