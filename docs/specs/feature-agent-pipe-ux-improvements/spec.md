---
type: feature-spec
title: "Agent and Pipe UX Improvements"
status: done
created: 2026-03-09
---

# Agent and Pipe UX Improvements

## Problem Statement

gcx has three UX gaps that degrade the experience for both human users piping output and AI agent consumers:

1. **No pipe detection.** When users pipe gcx output (e.g., `gcx list | jq .`), ANSI color codes and table column truncation pollute the output. Users must manually pass `--no-color` and there is no mechanism to suppress truncation. The `gh` CLI auto-detects piped stdout and adjusts behavior; gcx does not.

2. **No field discovery for JSON output.** Resources are `unstructured.Unstructured` with dynamic schemas. Users wanting specific fields via `--output json` have no way to discover what fields are available without making a request and inspecting the full JSON blob. The design guide marks field discovery as `[PLANNED]` (Section 1.5, R3.1).

3. **Errors are invisible to agents.** In agent mode, errors are written to stderr as colored human-formatted text (`DetailedError.Error()`). AI agents reading stdout for JSON responses never see error information. The design guide marks in-band error reporting as `[PLANNED]` (Section 4.4, R3.5). This means agents cannot programmatically detect, classify, or recover from errors.

**Who is affected:** Shell scripters piping gcx output; AI agents (Claude Code, Cursor, Copilot, Amazon Q) consuming gcx programmatically; CI/CD pipelines parsing gcx output.

**Current workarounds:** Pass `--no-color` manually; inspect full JSON output to guess field names; agents parse stderr text with regex (fragile and unreliable).

## Scope

### In Scope

- TTY detection on stdout using `term.IsTerminal()` in root `PersistentPreRun`
- Auto-disable color when stdout is piped (non-TTY)
- Auto-disable table column truncation when stdout is piped
- Suppress spinners/progress indicators when stdout is piped
- A `--json` flag that accepts a comma-separated field list for selective JSON output
- A field discovery mechanism (e.g., `--json ?` or `--json help`) that lists available fields by introspecting a sample resource
- In-band JSON error reporting when agent mode is active: errors written to stdout as structured JSON
- JSON error envelope that includes `summary`, `exitCode`, `details`, `suggestions`, and `docsLink` fields
- Backward compatibility with `--no-color`, `--agent`, `NO_COLOR`, and `-o json`

### Out of Scope

- Auto-format switching (piped stdout auto-switching default from `text` to `json`) -- requires broader design discussion, per design-guide Section 5.4
- Server-side field filtering (projecting fields at the API level) -- resources are unstructured; filtering is client-side only
- JMESPath or JSONPath query expressions -- this spec covers simple dot-path field selection only
- Confirmation prompt auto-approval in agent mode -- tracked separately per design-guide Section 3.3
- Spinner/progress indicator implementation -- none exist today; this spec covers the suppression contract for when they are added
- Changes to stderr output in non-agent mode -- human-formatted `DetailedError` rendering remains unchanged

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| TTY detection library | `golang.org/x/term` via `term.IsTerminal()` | Already used in `internal/graph/chart.go` for terminal size detection; avoids new dependency | Codebase (`internal/graph/chart.go:437`) |
| TTY check location | Root `PersistentPreRun` alongside existing color/agent logic | Must run before codec selection; single place for output mode decisions | design-guide Section 5.1 |
| Field selection flag | `--json field1,field2` (new flag, separate from `-o json`) | Matches `gh` CLI convention; `-o json` remains full-object output for backward compat | design-guide Section 1.5, gh CLI precedent |
| Field discovery syntax | `--json ?` prints available fields and exits | Single-character sentinel avoids collision with field names; no subcommand needed | User prompt |
| Field discovery backend | Discovery registry for resource schema resolution | Leverages existing API discovery infrastructure; avoids extra API calls per field query | User feedback |
| `--json` scope | Available on all structured-output commands (resource + non-resource) | Consistent UX; non-resource commands like `config view` benefit equally | User feedback |
| In-band error format | JSON object with `error` key written to stdout | Agents read stdout only; must be valid JSON so downstream `jq` does not break | design-guide Section 4.4 |
| Error + partial results | Errors alongside items in same JSON envelope | Agents need both partial results and error context in a single parse | design-guide Section 4.4 |
| Piped mode flag name | `--no-truncate` (explicit override for non-pipe use) | Allows users to disable truncation on TTY; piped mode auto-sets this | gh CLI precedent |
| Agent mode implies pipe | Agent mode auto-enables all pipe-aware behaviors | Agents always want clean, machine-parseable output regardless of TTY state | User feedback |

## Functional Requirements

### Pipe-Aware Output

FR-001: The system MUST detect whether stdout is a TTY by calling `term.IsTerminal()` on `os.Stdout.Fd()` during root `PersistentPreRun`, before codec selection occurs.

FR-002: WHEN stdout is not a TTY, the system MUST set `color.NoColor = true` to disable ANSI color codes in all output.

FR-003: WHEN stdout is not a TTY, the system MUST disable table column truncation in custom text/wide codecs. A package-level or context-propagated `IsPiped` boolean MUST be accessible to table codec implementations.

FR-004: The `--no-color` flag and `NO_COLOR` environment variable MUST continue to work and MUST take precedence over TTY auto-detection (i.e., `--no-color` forces no-color even on a TTY).

FR-005: The system MUST expose a `--no-truncate` flag that disables table column truncation regardless of TTY status.

FR-005a: WHEN agent mode is active, the system MUST automatically enable all pipe-aware behaviors (no color, no truncation, `IsPiped=true`) regardless of actual TTY status. Agent mode implies clean, machine-parseable output.

### JSON Field Selection and Discovery

FR-006: The system MUST support a `--json field1,field2,...` flag on all commands that produce structured output (both resource commands and non-resource commands like `config view`, `providers list`). When provided, the output format MUST be JSON containing only the specified fields from each output object.

FR-007: WHEN `--json ?` is provided, the system MUST require a resource selector argument (e.g., `gcx get dashboards --json ?`). The system MUST use the discovery registry to resolve the resource type's schema, extract all available field paths, print them one per line to stdout (sorted alphabetically), and exit with code 0. If no resource selector is provided, the system MUST produce a usage error.

FR-008: WHEN `--json` specifies a field name that does not exist in the resource object, the system MUST include that field in the output with a `null` value (not omit it, not error).

FR-009: The `--json` flag MUST be mutually exclusive with `-o/--output`. Providing both MUST produce a usage error.

FR-010: WHEN `--json` is used on a list operation returning multiple resources, the output MUST be a JSON object with an `items` array where each element contains only the selected fields.

### In-Band Error Reporting (Agent Mode)

FR-011: WHEN agent mode is active and a command fails, the system MUST write a JSON error object to stdout in addition to (not instead of) the existing stderr output. The JSON error object MUST contain: `error.summary` (string) and `error.exitCode` (integer).

FR-011a: The in-band error JSON object MAY include: `error.details` (string), `error.suggestions` (array of strings), and `error.docsLink` (string). These fields SHOULD be included when available from the `DetailedError`.

FR-012: WHEN agent mode is active and a command produces partial results with errors (e.g., batch operations with some failures), the output MUST be a single JSON object containing both `items` (array of successful results) and `error` (error object per FR-011).

FR-013: WHEN agent mode is active and a command succeeds, the system MUST NOT add an `error` key to the JSON output.

FR-014: The in-band error JSON MUST use the same exit code as the process exit code (derived from `DetailedError.ExitCode`).

## Acceptance Criteria

### Pipe Detection

- GIVEN gcx is invoked with stdout piped to another process (e.g., `gcx list dashboards | cat`)
  WHEN the command executes
  THEN stdout output MUST NOT contain ANSI escape sequences

- GIVEN gcx is invoked with stdout piped to another process
  WHEN the command produces table output (text codec)
  THEN no table column values are truncated with ellipsis

- GIVEN gcx is invoked on a TTY with `--no-color`
  WHEN the command executes
  THEN stdout output MUST NOT contain ANSI escape sequences (backward compat)

- GIVEN gcx is invoked on a TTY without `--no-color`
  WHEN the command executes
  THEN stdout output MAY contain ANSI escape sequences (existing behavior preserved)

- GIVEN gcx is invoked on a TTY with `--no-truncate`
  WHEN the command produces table output
  THEN no table column values are truncated with ellipsis

### JSON Field Selection

- GIVEN a gcx command that returns resource data
  WHEN `--json metadata.name,spec` is provided
  THEN stdout contains a JSON object with only the keys `metadata.name` and `spec` (and their values) for each resource

- GIVEN a gcx command that returns resource data
  WHEN `--json ?` is provided
  THEN stdout prints one field name per line (sorted alphabetically), and the process exits with code 0

- GIVEN a gcx command that returns resource data
  WHEN `--json nonexistent` is provided
  THEN stdout contains a JSON object where `nonexistent` is `null`

- GIVEN a gcx command
  WHEN both `--json field1` and `-o yaml` are provided
  THEN the command exits with a usage error (non-zero exit code) and an error message indicating mutual exclusivity

- GIVEN a gcx list command returning multiple resources
  WHEN `--json metadata.name` is provided
  THEN stdout contains `{"items": [{"metadata.name": "..."}, ...]}` with only the selected field per item

### In-Band Error Reporting

- GIVEN agent mode is active (via `--agent` or env var)
  WHEN a command fails with an auth error (HTTP 401)
  THEN stdout contains `{"error": {"summary": "...", "exitCode": 3, ...}}` as valid JSON
  AND stderr MUST NOT contain the human-formatted `DetailedError` output (machine consumers get JSON only)

- GIVEN agent mode is active
  WHEN a batch operation partially fails (e.g., push with some resource errors)
  THEN stdout contains `{"items": [...], "error": {"summary": "...", "exitCode": 4, ...}}`

- GIVEN agent mode is NOT active
  WHEN a command fails
  THEN stdout does NOT contain any error JSON (existing behavior preserved)

- GIVEN agent mode is active
  WHEN a command succeeds
  THEN stdout JSON does NOT contain an `error` key

## Negative Constraints

NC-001: The system MUST NEVER write ANSI escape sequences to stdout when stdout is not a TTY, regardless of flag state (exception: `--color=always` if added in the future, which is out of scope).

NC-002: The system MUST NEVER break backward compatibility of `-o json` output shape. Existing scripts using `-o json` MUST receive the same full-object JSON they receive today. The `--json` flag is an independent mechanism.

NC-003: [REVISED] WHEN agent mode or `--json` is active, the system MUST write JSON errors to stdout only and MUST NOT write the human-formatted `DetailedError` to stderr. Human-formatted stderr errors are reserved for normal (non-machine) mode. If JSON serialization fails, the system SHOULD fall back to stderr as best-effort.

NC-004: The system MUST NEVER produce invalid JSON to stdout when `--json` or agent-mode error reporting is active. Partial writes, color codes, or status messages MUST NOT corrupt the JSON stream.

NC-005: The system MUST NEVER perform an additional API request solely for `--json ?` field discovery beyond the single introspection fetch. Field discovery MUST NOT trigger full list operations.

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Table codecs do not have a unified truncation control point | Each command's custom table codec may implement truncation independently; piped-mode signal may not reach all codecs | Audit all custom codecs during implementation; introduce a shared `TableOptions` struct that codecs read |
| Dynamic schema makes `--json ?` output unpredictable | Different resources have different fields; `--json ?` on `dashboards` vs `folders` yields different results | Document that `--json ?` shows fields for the discovered resource type; require resource selector argument |
| Agent-mode error JSON interleaves with command stdout | If a command writes partial JSON to stdout before failing, the error JSON appended afterward may produce invalid JSON | Route all stdout writes through a buffered writer in agent mode; flush either success JSON or error JSON, never both partially |
| `term.IsTerminal()` returns incorrect results in some CI environments | Some CI runners (GitHub Actions) report stdout as TTY | Document that `--no-color` and `--no-truncate` flags are the reliable override; TTY detection is best-effort |
| `--json` flag name collision with future Cobra or k8s conventions | Other tools may use `--json` differently | Prefix with clear help text; the flag is additive and does not replace `-o` |

## Open Questions

- [RESOLVED] Should `--json` support nested dot-path field selection (e.g., `metadata.name`)? **Yes** — resources are nested maps; top-level-only selection is too coarse for `metadata` subfields.
- [RESOLVED] Should piped mode auto-switch default format from `text` to `json`? **No** — marked out of scope per design-guide Section 5.4; requires separate design discussion.
- [RESOLVED] Should `--json ?` require a resource selector? **Yes** — `--json ?` requires a resource selector (e.g., `gcx get dashboards --json ?`). For resource commands, the discovery registry resolves the resource type to its schema.
- [RESOLVED] Should `--json` be available on non-resource commands (e.g., `config view`, `providers list`)? **Yes** — `--json` applies to all commands that produce structured output, not just resource commands.
- [RESOLVED] Should agent mode auto-switch to `-o json`? **Already implemented** — `io.Options.BindFlags()` already sets JSON as the default output format when `agent.IsAgentMode()` is true. Users can override with explicit `-o text` etc. No changes needed.
- [DEFERRED] Should the in-band error envelope support a `warnings` array for non-fatal issues (e.g., deprecated API versions)? Defer to a follow-up spec on structured diagnostics.
