---
type: feature-tasks
title: "Agent and Pipe UX Improvements"
status: draft
spec: docs/specs/feature-agent-pipe-ux-improvements/spec.md
plan: docs/specs/feature-agent-pipe-ux-improvements/plan.md
created: 2026-03-09
---

# Implementation Tasks

## Dependency Graph

```
T1 (TTY detection) ──► T2 (pipe-aware codecs)
                   ──► T3 (--json flag parsing)
                   ──► T5 (in-band errors)

T3 (--json flag) ──► T4 (field select + discovery)

T2, T4, T5 ──► T6 (integration tests)
          ──► T7 (documentation)
```

## Wave 1: Foundation

### T1: TTY Detection and Global Pipe State
**Priority**: P0
**Effort**: Small
**Depends on**: none
**Type**: task

Add TTY detection to root `PersistentPreRun` using `term.IsTerminal(os.Stdout.Fd())`. Store the result as package-level state in a new `internal/terminal` package (similar to `internal/agent`). Set `color.NoColor = true` when stdout is not a TTY. Add `--no-truncate` persistent flag to root command. Ensure `--no-color` and `NO_COLOR` env var continue to take precedence over TTY auto-detection (fatih/color already respects `NO_COLOR`). When `agent.IsAgentMode()` is true, automatically enable all pipe-aware behaviors (`IsPiped=true`, `NoTruncate=true`, `color.NoColor=true`) regardless of actual TTY status.

**Deliverables:**
- `internal/terminal/terminal.go` — `IsPiped() bool`, `SetPiped(bool)`, `NoTruncate() bool`, `SetNoTruncate(bool)`, detection via `term.IsTerminal()`
- `internal/terminal/terminal_test.go` — unit tests
- Modified `cmd/grafanactl/root/command.go` — TTY check in PersistentPreRun, `--no-truncate` flag

**Acceptance criteria:**
- GIVEN stdout is a pipe (non-TTY) WHEN grafanactl runs any command THEN `color.NoColor` is set to `true` and `terminal.IsPiped()` returns `true`
- GIVEN stdout is a TTY WHEN grafanactl runs any command without `--no-color` THEN `color.NoColor` remains `false` and `terminal.IsPiped()` returns `false`
- GIVEN stdout is a TTY WHEN `--no-color` flag is passed THEN `color.NoColor` is `true` (flag takes precedence over TTY detection)
- GIVEN stdout is a pipe WHEN `--no-truncate` flag is NOT passed THEN `terminal.NoTruncate()` returns `true` (auto-detected from pipe)
- GIVEN stdout is a TTY WHEN `--no-truncate` flag is passed THEN `terminal.NoTruncate()` returns `true` (explicit override)

---

## Wave 2: Feature Implementation

### T2: Pipe-Aware Table Codecs
**Priority**: P0
**Effort**: Medium
**Depends on**: T1
**Type**: task

Update `cmd/grafanactl/io.Options` to expose `IsPiped` and `NoTruncate` booleans populated from `internal/terminal` state. Modify table codecs that use `text/tabwriter` to skip column truncation when `NoTruncate` is true. The pipe-aware behavior applies to the `text` and `wide` codecs across all commands that register them.

Since table codecs are instantiated per-command (via `RegisterCustomCodec`), codecs can read `terminal.IsPiped()` / `terminal.NoTruncate()` directly from the package-level state.

**Deliverables:**
- Modified `cmd/grafanactl/io/format.go` — `IsPiped`, `NoTruncate` fields on Options; populated from terminal state
- Modified `cmd/grafanactl/resources/get.go` — tableCodec reads NoTruncate
- Modified `cmd/grafanactl/resources/list.go` — tabCodec reads NoTruncate
- Spot-check 2-3 provider table codecs to confirm pattern works

**Acceptance criteria:**
- GIVEN stdout is piped (non-TTY) WHEN `grafanactl resources get dashboards` runs with default text output THEN table columns are NOT truncated and output contains no ANSI escape sequences
- GIVEN stdout is a TTY WHEN `grafanactl resources get dashboards` runs with default text output THEN table columns behave as they do today (truncation active where applicable)
- GIVEN stdout is a TTY and `--no-truncate` is passed WHEN `grafanactl resources get dashboards` runs THEN table columns are NOT truncated

---

### T3: --json Flag Parsing and Mutual Exclusion
**Priority**: P0
**Effort**: Medium
**Depends on**: T1
**Type**: task

Add `--json` flag to `cmd/grafanactl/io.Options`. The flag accepts a comma-separated list of field names or the sentinel `?`. Store parsed fields in `Options.JSONFields []string`. In `Validate()`, enforce mutual exclusion: if `--json` is set and `-o/--output` is also explicitly set, return an error. When `--json` is set (non-`?`), override the output format to JSON internally. When `--json ?` is used, set a boolean `Options.JSONDiscovery` that commands check to trigger field discovery instead of normal execution.

**Deliverables:**
- Modified `cmd/grafanactl/io/format.go` — `JSONFields`, `JSONDiscovery` fields; `--json` flag binding; mutual exclusion validation
- Modified `cmd/grafanactl/io/format_test.go` — tests for flag parsing, mutual exclusion, sentinel detection

**Acceptance criteria:**
- GIVEN `--json name,namespace,kind` is passed WHEN `Options.Validate()` runs THEN `JSONFields` contains `["name", "namespace", "kind"]` and no error
- GIVEN `--json name` and `-o yaml` are both passed WHEN `Options.Validate()` runs THEN an error is returned stating mutual exclusion
- GIVEN `--json ?` is passed WHEN `Options.Validate()` runs THEN `JSONDiscovery` is `true` and no error
- GIVEN `--json` is not passed WHEN `Options.Validate()` runs THEN `JSONFields` is nil and `JSONDiscovery` is `false`

---

### T4: Field Selection Codec and Field Discovery
**Priority**: P1
**Effort**: Medium-Large
**Depends on**: T3
**Type**: task

Implement `FieldSelectCodec` in `cmd/grafanactl/io/` that wraps `format.JSONCodec`. When `JSONFields` is set on Options, `Encode()` extracts only the requested fields from the input data. For `unstructured.Unstructured` objects, walk the object map. For other types, marshal to JSON then extract fields. Missing fields produce `null` values. For list results, wrap in `{"items": [...]}`.

Implement `--json ?` field discovery: when `JSONDiscovery` is true, the command fetches one resource instance via the discovery registry, enumerates its top-level and `spec.*` field paths, prints them as a sorted list to stdout, and exits with code 0. Add this logic to `resources get` and `resources list` commands as the initial implementation.

**Deliverables:**
- `cmd/grafanactl/io/field_select.go` — `FieldSelectCodec` type
- `cmd/grafanactl/io/field_select_test.go` — unit tests for field extraction, null handling, list wrapping
- Modified `cmd/grafanactl/resources/get.go` — wire `--json ?` discovery; use `FieldSelectCodec` when `JSONFields` set
- Modified `cmd/grafanactl/resources/list.go` — wire `--json ?` discovery; use `FieldSelectCodec` when `JSONFields` set

**Acceptance criteria:**
- GIVEN `--json name,namespace` is passed to `resources get dashboards` WHEN the command runs THEN output is JSON containing only `name` and `namespace` fields per item
- GIVEN `--json nonexistent` is passed to `resources get dashboards` WHEN the command runs THEN output contains `"nonexistent": null` and the command exits with code 0
- GIVEN `--json name` is passed to a list command returning multiple items WHEN the command runs THEN output is `{"items": [{"name": "..."}, ...]}` shape
- GIVEN `--json ?` is passed with a valid resource selector WHEN the command runs THEN a sorted list of available field paths prints to stdout and exit code is 0

---

### T5: In-Band JSON Error Reporting for Agent Mode
**Priority**: P1
**Effort**: Medium
**Depends on**: T1
**Type**: task

Modify `handleError()` in `cmd/grafanactl/main.go` to emit a JSON error object to stdout when in agent mode (`agent.IsAgentMode()`) or when `--json` was used. The JSON object has the shape `{"error": {"summary": "...", "exitCode": N, "details": "...", "suggestions": [...], "docsLink": "..."}}`. Optional fields are omitted when empty. The `exitCode` in JSON MUST match the process exit code. Stderr output remains unchanged (human-formatted error still written).

For partial failure scenarios (e.g., `resources get` with `--on-error=continue`), ensure the error envelope wraps around any previously written output. This requires commands that support partial failures to buffer output and use a result envelope.

**Deliverables:**
- Modified `cmd/grafanactl/main.go` — JSON error to stdout in `handleError()`
- `cmd/grafanactl/fail/json.go` — `ToJSON()` method on `DetailedError` producing the error JSON structure
- `cmd/grafanactl/fail/json_test.go` — unit tests
- Modified `cmd/grafanactl/fail/detailed.go` — no breaking changes, add JSON serialization support

**Acceptance criteria:**
- GIVEN agent mode is active WHEN a command fails with a DetailedError THEN stdout contains `{"error": {"summary": "...", "exitCode": N}}` and stderr contains the human-formatted error
- GIVEN agent mode is active WHEN a command succeeds THEN stdout contains no `error` key
- GIVEN `--json name` is passed (non-agent mode) WHEN a command fails THEN stdout contains the JSON error object (because `--json` implies structured output)
- GIVEN agent mode is active and a DetailedError has suggestions and docsLink WHEN the error is serialized THEN the JSON includes `suggestions` array and `docsLink` string
- GIVEN a command fails with exit code 3 (auth failure) WHEN the JSON error is written THEN `error.exitCode` is 3 and the process exits with code 3

---

## Wave 3: Integration and Polish

### T6: Integration Tests
**Priority**: P1
**Effort**: Medium
**Depends on**: T2, T4, T5
**Type**: task

Write integration-style tests that verify end-to-end behavior of all three features. Use `cmd/grafanactl/root.Command()` directly with programmatic stdout/stderr capture. Test pipe detection by controlling the file descriptor (use `os.Pipe()` to simulate piped stdout). Test `--json` field selection with mock discovery data. Test in-band error reporting with intentional failures.

**Deliverables:**
- `cmd/grafanactl/io/integration_test.go` — pipe detection + truncation tests
- `cmd/grafanactl/io/json_fields_integration_test.go` — `--json` end-to-end tests
- `cmd/grafanactl/fail/error_integration_test.go` — in-band error tests

**Acceptance criteria:**
- GIVEN a test that executes a command with stdout set to `os.Pipe()` WHEN the command produces table output THEN the output contains no ANSI codes and columns are not truncated
- GIVEN a test that executes `resources get dashboards --json name,kind` WHEN mock resources are returned THEN output JSON contains only `name` and `kind` fields
- GIVEN a test that triggers an auth error in agent mode WHEN the error is handled THEN stdout JSON contains `{"error": {"summary": "...", "exitCode": 3}}` and process exit would be 3

---

### T7: Documentation Updates
**Priority**: P2
**Effort**: Small
**Depends on**: T2, T4, T5
**Type**: chore

Update agent-docs and design-guide to document the three new features. Add pipe detection behavior to `design-guide.md` section on output. Document `--json` flag usage with examples. Document in-band error format for agent consumers. Update `cli-layer.md` codec documentation to cover `FieldSelectCodec` and pipe-aware behavior.

**Deliverables:**
- Modified `agent-docs/design-guide.md` — pipe detection, `--json` flag, in-band errors sections
- Modified `agent-docs/cli-layer.md` — updated codec registry docs, `--json` flag docs
- Modified `agent-docs/patterns.md` — new pattern for pipe-aware output if appropriate

**Acceptance criteria:**
- WHEN a developer reads `design-guide.md` THEN they find documentation for pipe detection behavior, `--json` flag syntax, and agent error format
- WHEN a developer reads `cli-layer.md` THEN they find updated codec documentation covering `FieldSelectCodec` and `--no-truncate`
