# Design: gcx

> Shared vocabulary for how gcx commands look and feel — for developers and agents implementing new commands or providers.
>
> This file covers philosophy and intent. Prescriptive implementation rules are in [docs/design/](docs/design/).
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
gcx irm oncall schedules get my-schedule
```

See [CONSTITUTION.md § CLI Grammar](CONSTITUTION.md#cli-grammar) for the authoritative rules (positional arguments, flags, extension commands, verb constraints).

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

See [docs/design/agent-mode.md](docs/design/agent-mode.md) for detection logic and opt-out.

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

See [docs/design/output.md](docs/design/output.md) for codec implementation, status messages, and mutation summaries.

## Verbosity and Diagnostic Output

gcx is quiet by default — stdout carries only the requested data, stderr carries
nothing unless something goes wrong. Use `-v` flags to increase diagnostic output.

### Log level ladder

The root `--verbose` / `-v` flag maps to `slog` levels. Each additional `-v`
lowers the threshold by one level (4 points in slog's scale):

| Flags | slog threshold | What you see |
|-------|---------------|--------------|
| (none) | Error | Only hard errors — the command failed |
| `-v` | Warn | HTTP 5xx responses, transport failures, deprecation warnings |
| `-vv` | Info | Notable events — config loading, discovery, auth |
| `-vvv` | Debug | Every HTTP request/response (method, URL, status) |

Maximum is `-vvv`; additional flags beyond three have no effect.

### HTTP request logging

With `-vvv`, every outbound HTTP call logs at Debug:

```
DEBUG http request method=GET url=https://...
DEBUG http response method=GET url=https://... status=200
```

5xx responses and transport errors (connection refused, timeout, TLS) surface
at Warn — visible with just `-v`:

```
WARN http response method=GET url=https://... status=502
WARN http error   method=GET url=https://... error="connection refused"
```

### `--log-http-payload`

Dumps the full request and response bodies (via `httputil.DumpRequest` /
`httputil.DumpResponse`) at Debug level. Requires `-vvv` to be visible.

```
gcx --log-http-payload -vvv slo list
```

**Warning:** The dump includes all headers, including `Authorization`. Treat
the output as sensitive — do not paste it into public issues or logs.

This flag applies to all HTTP tiers — both the provider tier
(`httputils.NewDefaultClient`) and the K8s resource tier (transport chain built
by `NewNamespacedRESTConfig` via `WrapTransport`).

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

See [docs/design/exit-codes.md](docs/design/exit-codes.md) for implementation with `DetailedError` and converters.

## Safety Patterns

- **Idempotent by default**: `push` is create-or-update. Safe to run repeatedly.
- **Dry-run available**: `push` and `delete` accept `--dry-run`.
- **Prompt before destructive ops**: `delete` prompts unless `--yes`/`-y` or `GCX_AUTO_APPROVE`.
- **No prompt for reversible ops**: push, pull, config changes do not prompt.

See [docs/design/safety.md](docs/design/safety.md) for implementation patterns and flag precedence.

## Taste Rules

Code taste rules (options pattern, error messages, test style, commit format) live in [ARCHITECTURE.md § Taste Rules](ARCHITECTURE.md#taste-rules). Authoritative source: [CONSTITUTION.md § Taste Rules](CONSTITUTION.md#taste-rules).

## Implementation Guides

Prescriptive implementation rules live in [docs/design/](docs/design/), split by domain:

| Document | Domain |
|----------|--------|
| [output.md](docs/design/output.md) | Output codecs, status messages, JSON field selection, mutation summaries |
| [exit-codes.md](docs/design/exit-codes.md) | Exit code taxonomy and implementation |
| [safety.md](docs/design/safety.md) | Confirmation prompts, `--yes`, dry-run, idempotency |
| [errors.md](docs/design/errors.md) | DetailedError structure, converters, in-band JSON errors |
| [pipe-awareness.md](docs/design/pipe-awareness.md) | TTY detection, `--no-color`, pipe behavior |
| [agent-mode.md](docs/design/agent-mode.md) | Agent mode detection, behavior changes, opt-out |
| [provider-checklist.md](docs/design/provider-checklist.md) | Provider UX compliance (architecture patterns in [patterns.md](docs/architecture/patterns.md)) |
| [help-text.md](docs/design/help-text.md) | Command descriptions, examples format |
| [naming.md](docs/design/naming.md) | Resource kinds, file naming, config keys, flags |
| [environment-variables.md](docs/design/environment-variables.md) | Canonical environment variable reference |
