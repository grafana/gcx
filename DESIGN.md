# Design: gcx

> Shared vocabulary for how gcx commands look and feel — for developers and agents implementing new commands or providers.
>
> This file states the CLI and agent-facing rules and why they exist. The documents in [docs/design/](docs/design/) hold the mechanics of following them.
>
> The rules apply to all new code. Where a shipped command breaks one, the violation is tracked migration debt, in a test allowlist or under [#1694](https://github.com/grafana/gcx/issues/1694); it does not license a second one. Waiving a rule needs maintainer approval in the PR.

## Philosophy

gcx is a dual-purpose tool. Every command serves both human operators and AI agents running in pipelines. We optimize for:

- **Predictability** — consistent command grammar, consistent output shapes, consistent error format
- **Composability** — shell-pipeable, machine-parseable by default in agent mode, stable exit codes
- **Transparency** — errors tell you what failed and suggest how to fix it; warnings are explicit, not silent

## CLI Grammar

```
gcx slo definitions list
gcx resources push ./dashboards/
gcx logs query --from=1h
gcx irm oncall schedules get my-schedule
```

- **Resource and provider commands read `gcx $AREA $NOUN $VERB`.** An area with
  no meaningful noun, such as `config` or `dev`, may use `$AREA $VERB`. A command
  outside the grammar cannot be guessed, and agents cannot find it from its
  neighbors. Verbs and placement: [command-naming.md](docs/design/command-naming.md).
- **Bare top-level verbs are a closed list.** Only bootstrapping commands that
  come before any area (`signup`, `login`, `setup`) and commands about the binary
  itself (`version`, `help`, `completion`) may be a single token. A new top-level
  command uses an area; nothing qualifies by analogy.
- **Extension commands nest under their resource type.** Domain operations such
  as `status`, `timeline` or `acknowledge` sit next to the CRUD verbs. An
  extension never duplicates a CRUD verb: two ways to create a resource means
  two code paths and two output shapes.
- **Operations describe behavior.** A real read-one is `get` and a real
  enumeration is `list`, whether or not an adapter backs the resource. Anything
  else uses an honest domain verb or an `<operation>-<subject>` compound such as
  `list-profile-types`. A command that dresses an action up as CRUD lies about
  what it does.
- **Sub-resources nest under their parent.** A resource that cannot be listed or
  addressed without a parent ID is exposed as `$PARENT $VERB-$CHILD $PARENT_ID`
  (`alert-groups list-alerts <id>`), never as a standalone adapter whose list
  ignores the parent and returns an unscoped dump.
- **Positional arguments name the subject; flags modify.** Selectors, UIDs,
  expressions and paths are positional. Format, concurrency, dry-run and filters
  are flags. A subject passed as a flag breaks completion and every skill that
  composes commands positionally.
- **The command surface is stable within a major version.** From v1.0.0,
  command paths, aliases, flags and positional syntax keep working until the
  next major release, including invocations that predate these rules. A grammar
  rule never justifies renaming a released command: add the preferred form and
  keep the old one working. A command marked experimental before release is
  exempt; see [experimental-commands.md](docs/design/experimental-commands.md).

## Dual-Purpose Design

Every command serves both humans and agents.

- **Agent mode changes defaults and never removes a capability.** It switches to
  JSON output, no color, no truncation and plain ASCII. An interaction that
  cannot be automated, such as a confirmation, an editor or a picker, becomes a
  required input instead: a flag the agent passes. Every such case is listed in
  [agent-mode.md](docs/design/agent-mode.md).
- **Explicit flags beat agent-mode defaults.** `-o yaml` means YAML in agent
  mode too. The non-format defaults apply to every format, so no codec puts
  escape codes or box characters in front of an agent.
- **Commands that write a file format pin their default** with
  `PinDefaultFormat`. Agent mode would otherwise switch the files on disk to its
  own output format. Mechanics: [agent-mode.md § 6.4](docs/design/agent-mode.md).

| Aspect | Human mode | Agent mode |
|--------|-----------|------------|
| Default output | the command's narrow table codec | `agents` (compact JSON with spill) |
| Colors | On (TTY) | Off |
| Truncation | On (TTY) | Off |
| Prompts | Interactive | Non-destructive prompts use defaults; destructive actions need `--force` or enabled `GCX_AUTO_APPROVE` (see [safety.md](docs/design/safety.md) § 3.3) |

Agent mode is active when `GCX_AGENT_MODE=true`, or when gcx detects a native
agent marker or a supported explicit identity. See the
[agent environment reference](docs/design/environment-variables.md#agent-mode-variables).
An explicitly passed `--agent`/`--agent=false` overrides environment detection
in both directions; see [agent-mode.md § 6.1](docs/design/agent-mode.md#61-detection).

### The agent output contract

When agent mode supplies the default (no explicit `--output`, `--json` or
`--jq`):

- **A finite command emits exactly one JSON value on stdout**, its result or a
  fused error, and the exit code agrees with that outcome. A second value, or a
  success code on an error document, breaks every agent parsing the output.
- **A stream emits typed, versioned JSONL through the shared event writer**,
  ending in a terminal success or error event. Without that event a consumer
  cannot tell a finished stream from a dropped connection.
- **Every other command declares its protocol class** in
  `cmd/gcx/root/testdata/output_classes.json`: artifact, interactive, server,
  raw, shell or prose. CI fails on a command with no class.
- **gcx-owned envelopes carry `type` and `schema_version`**, so a consumer can
  route a document it has not seen before. Envelopes that shipped before the
  contract keep their form; see [output.md](docs/design/output.md).

Class definitions, detection and opt-out: [agent-mode.md](docs/design/agent-mode.md).

## Output Model

- **stdout carries the result; stderr carries advisory diagnostics only.**
  Resource data, operation summaries and failure details that a structured
  result needs go to stdout. Progress, warnings and hints go to stderr through
  the shared output and logging helpers. A result on stderr makes
  `gcx ... | jq` return nothing, and a direct write to `os.Stderr` escapes the
  agent-mode framing.
- **Primary output goes through the shared output system.** Finite commands
  encode through codecs, with no ad-hoc prose on stdout. Artifact commands
  encode file contents with codecs and finish with an `EmitArtifactResult`
  receipt. A hand-written print bypasses `-o`, `--json`, `--jq` and agent mode at
  once.
- **Output shapes data and never affects it.** `-o` chooses how the fetched data
  is presented; it never changes what is fetched. A different payload, such as a
  raw profile instead of a flame graph, is a different command. Otherwise JSON
  silently loses fields that only one format asked for. Mechanics:
  [patterns.md § 13](docs/architecture/patterns.md#13-format-agnostic-data-fetching).
- **Provider commands and `gcx resources` return the same JSON and YAML** for the
  same resource. Tables may differ: provider tables show domain columns. A
  skill written against one path must work on the other.
- **Batch results count successes and skips, and list failures.** Listing 5,000
  successes fills an agent's context; listing failures tells the caller what to
  act on. Mechanics: [output.md § 12](docs/design/output.md#12-mutation-command-output).
- **Query commands link to Grafana Explore.** Every query-class datasource
  command builds an Explore URL and supports `--share-link` and `--open`, so a
  user can see the same query in Grafana. Mechanics:
  [datasource implementation](.agents/skills/contribute/references/datasource-implementation.md#step-1c-explore-link-required).

Default formats by command type:

| Command type | Default | Rationale |
|-------------|---------|-----------|
| `list`, `get` | a narrow table codec (`text` or `table`) | Human-scannable |
| `config view` | `yaml` | Config is YAML-native |
| `push`, `delete` | structured operation summary | Operations, not data |
| `resources pull` | `json` (pinned file format) | Files are the output; deprecated SLO pulls retain fixed YAML |
| Agent mode | `agents` | Compact JSON with temp-file spill |

There is no repo-wide format set — read a command's own `-o` line, and see
[output.md § 1.3](docs/design/output.md) for the rule new commands follow.

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

### `--insecure-log-http-payload`

Dumps the full request and response bodies (via `httputil.DumpRequestOut` /
`httputil.DumpResponse`) at Debug level. Requires `-vvv` to be visible.

```
gcx --insecure-log-http-payload -vvv slo list
```

Each dump carries a label, so you can find it in the log: `http request dump`
and `http response dump`. A wire dump holds no word that identifies it, so
searching for "body" finds nothing.

The dump is the innermost transport layer, so it shows every header that an
outer layer adds, including the bearer token that the OAuth transport adds. The
dump renders an HTTP/1.1 request line, so the framing is not exact on an HTTP/2
connection. The dump also covers the OAuth token refresh exchange, because
`auth.RefreshTransport` sends the refresh request through the same inner layer.
When a refresh fails, gcx never sends your original request, so only the refresh
exchange appears. The `WARN http error` line carries the reason.

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
| 2 | Usage error | Invalid selectors, unknown commands, missing required flags — see [exit-codes.md § 2.3](docs/design/exit-codes.md) for what Cobra still surfaces as 1 |
| 3 | Auth failure | 401/403, missing or invalid credentials |
| 4 | Partial failure | Some resources succeeded, others failed |
| 5 | Cancelled | The invocation stopped early: Ctrl+C, `context.Canceled`, explicit confirmation cancellation, or server-reported cancellation; see [legacy confirmation exceptions](docs/design/exit-codes.md#24-declined-confirmations) |
| 6 | Version incompatible | Grafana < 12 detected |

See [docs/design/exit-codes.md](docs/design/exit-codes.md) for implementation with `DetailedError` and converters.

## Safety Patterns

- **Idempotent by default**: `push` is create-or-update. Safe to run repeatedly.
- **Dry-run available**: `resources push` and `resources delete` accept `--dry-run`; provider commands document their own support.
- **Prompt before destructive operations**: cloud provider delete commands prompt for confirmation unless `--force` or enabled `GCX_AUTO_APPROVE` bypasses it; agent mode rejects a destructive operation without either bypass. `resources delete` uses its existing selector guard instead of a prompt: type-only selectors require `--force` (`--yes` also enables it).
- **No prompt for reversible ops**: push, pull, config changes do not prompt.

See [docs/design/safety.md](docs/design/safety.md) for implementation patterns and flag precedence.

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
| [command-naming.md](docs/design/command-naming.md) | Canonical command verbs and placement |
| [experimental-commands.md](docs/design/experimental-commands.md) | Marking experimental commands in help text and agent metadata |
| [environment-variables.md](docs/design/environment-variables.md) | Canonical environment variable reference |
