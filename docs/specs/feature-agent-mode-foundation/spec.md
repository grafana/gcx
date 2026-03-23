---
type: feature-spec
title: "Agent Mode Foundation"
status: done
beads_id: grafanactl-experiments-pt8
created: 2026-03-06
---

# Agent Mode Foundation

## Problem Statement

grafanactl is a human-oriented CLI that defaults to colorized, human-readable
output formats (text, table, pretty). When AI coding agents (Claude Code, Cursor,
GitHub Copilot, Amazon Q) invoke grafanactl, they need machine-parseable output
(JSON), no ANSI escape codes, and predictable exit codes that convey failure
categories without parsing stderr text. Today, agents must remember to pass
`-o json --no-color` on every invocation and cannot distinguish auth failures
from version mismatches from user interrupts -- everything exits 1.

This feature establishes the foundation layer that makes grafanactl a
first-class tool for AI agents by (a) auto-detecting agent execution
environments and adjusting defaults, and (b) implementing a differentiated
exit code taxonomy.

## Scope

### In Scope

- **Agent mode detection package** (`internal/agent/`): env var detection and
  `--agent` flag support.
- **Agent mode behavioral effects**: default output format switches to JSON,
  color is disabled.
- **Exit code taxonomy**: five distinct exit codes (0-4) assigned to
  categorized error conditions.
- **Signal handler**: SIGINT produces exit code 2 (Cancelled).
- **Unit and integration tests** for both subsystems.

### Out of Scope

- Auto-approve for destructive operations (Phase 4).
- Spinner/progress bar suppression (no spinners exist today; this becomes
  relevant when spinners are added).
- Agent-specific error message formatting (structured JSON errors).
- Telemetry or analytics for agent mode usage.
- New CLI commands or subcommands for agent interactions.

## Key Decisions

| ID | Decision | Rationale |
|----|----------|-----------|
| KD-1 | Pre-parse `os.Args` for `--agent` in `main.go` before `cobra.Execute()` | `io.Options.BindFlags()` runs during command construction (before `PersistentPreRun`). The agent mode state must be available at `BindFlags()` time to override the default format. Cobra's flag parsing has not run yet, so we manually scan `os.Args`. |
| KD-2 | Env var detection runs at package `init()` time in `internal/agent` | Same timing constraint as KD-1. `init()` executes before `main()`, making `agent.IsAgentMode()` available when `BindFlags()` is called. The `--agent` flag is merged in during `main()` pre-parse. |
| KD-3 | `--agent` is a persistent root flag (not a hidden env-only toggle) | Explicit opt-in is important for debugging and for agents that cannot set env vars. Persistent flag ensures it works on any subcommand. |
| KD-4 | Exit code 5 = Cancelled (SIGINT); codes 2 and 4 kept for design-guide-defined usage/partial-failure | `docs/reference/design-guide.md` Section 2.1 already reserves codes 2 (usage error) and 4 (partial failure). Rather than conflict, this phase extends the taxonomy with codes 5 and 6. |
| KD-5 | Exit code taxonomy spans 0-6 | Codes 0-4 follow the design-guide; codes 5 and 6 are new additions in this phase for agent-relevant failure categories. |
| KD-6 | `-o text` overrides JSON default in agent mode | Explicit user/agent flags always take precedence over inferred defaults. This prevents agent mode from being a jail. |

## Functional Requirements

### Agent Mode Detection

- **FR-001**: The system MUST detect agent mode when any of these environment
  variables is set to a truthy value (`1`, `true`, `yes`):
  `CLAUDE_CODE`, `CURSOR_AGENT`, `GITHUB_COPILOT`, `AMAZON_Q`,
  `GRAFANACTL_AGENT_MODE`.
  If `GRAFANACTL_AGENT_MODE` is explicitly set to a falsy value (`0`, `false`,
  `no`), agent mode MUST be disabled regardless of other agent env vars.
- **FR-002**: The system MUST detect agent mode when the `--agent` persistent
  flag is passed on the root command.
- **FR-003**: The `internal/agent` package MUST expose `IsAgentMode() bool`
  that returns the resolved agent mode state.
- **FR-004**: The `internal/agent` package MUST expose `SetFlag(bool)` (or
  equivalent) so that `main.go` can merge the `--agent` flag result after
  pre-parsing `os.Args`.
- **FR-005**: Env var detection MUST happen at package `init()` time so
  that `IsAgentMode()` is accurate when `io.Options.BindFlags()` runs.

### Agent Mode Behavioral Effects

- **FR-006**: When agent mode is active, `io.Options.BindFlags()` MUST use
  `"json"` as the default output format regardless of any per-command
  `DefaultFormat()` override.
- **FR-007**: When agent mode is active, `PersistentPreRun` MUST set
  `color.NoColor = true`.
- **FR-008**: An explicit `-o <format>` flag MUST override the agent-mode
  JSON default.
- **FR-009**: Agent mode MUST NOT alter behavior when the user has not
  triggered it (zero behavioral change for non-agent users).

### Exit Code Taxonomy

- **FR-010**: Exit code 0 MUST indicate success (no change from current).
- **FR-011**: Exit code 1 MUST indicate general/unclassified failure
  (default, no change from current).
- **FR-012**: Exit code 2 is reserved for usage errors per `design-guide.md`
  Section 2.1 and MUST NOT be used in this phase.
- **FR-013**: Exit code 3 MUST indicate authentication/authorization failure.
  `convertAPIErrors` MUST set `ExitCode` to 3 for HTTP 401 and 403 responses.
- **FR-014**: Exit code 4 is reserved for partial failures per `design-guide.md`
  Section 2.1 and MUST NOT be used in this phase.
- **FR-015**: Exit code 5 MUST indicate cancellation. The system MUST install
  a SIGINT signal handler that produces exit code 5. The `handleError`
  function in `main.go` MUST detect `context.Canceled` errors (including
  wrapped errors via `errors.Is`) and map them to exit code 5. The
  implementation MUST add a `convertCanceledErrors` function to
  `cmd/grafanactl/fail/convert.go` or detect it inline in `handleError`.
- **FR-016**: Exit code 6 MUST indicate version incompatibility. When Grafana
  version < 12 is detected, the resulting error MUST carry exit code 6.
- **FR-017**: Exit code constants MUST be defined in a central location
  (`cmd/grafanactl/fail/exitcodes.go`), covering the full range 0-6 with
  named constants.
- **FR-018**: After implementation, `docs/reference/design-guide.md` Section 2.1
  MUST be updated to include exit codes 5 and 6, and Section 6.1 MUST list
  all five agent detection env vars (`CLAUDE_CODE`, `CURSOR_AGENT`,
  `GITHUB_COPILOT`, `AMAZON_Q`, `GRAFANACTL_AGENT_MODE`).

## Acceptance Criteria

### Agent Mode Detection

- **AC-001**: Given `CLAUDE_CODE=1` is set, When `grafanactl resources list`
  is executed without `-o`, Then the default output format is JSON.
- **AC-002**: Given no agent env vars are set, When `grafanactl --agent
  resources list` is executed, Then agent mode is active and output defaults
  to JSON.
- **AC-003**: Given `CLAUDE_CODE=1` is set, When `grafanactl resources list
  -o text` is executed, Then the output format is text (explicit flag wins).
- **AC-004**: Given no agent env vars are set and `--agent` is not passed,
  When `grafanactl resources list` is executed, Then the default output
  format is the per-command default (text for `list`), not JSON.
- **AC-005**: Given `GRAFANACTL_AGENT_MODE=1` is set, When `IsAgentMode()`
  is called, Then it returns true.

### Exit Codes

- **AC-006**: Given the Grafana API returns HTTP 401, When a command fails,
  Then the process exits with code 3.
- **AC-007**: Given the Grafana API returns HTTP 403, When a command fails,
  Then the process exits with code 3.
- **AC-008**: Given the user presses Ctrl+C (SIGINT), When the signal is
  caught, Then the process exits with code 5.
- **AC-009**: Given a `context.Canceled` error propagates to `handleError`,
  When it is detected, Then the process exits with code 5.
- **AC-010**: Given a Grafana version < 12 is detected and an error is
  returned, When that error propagates to `handleError`, Then the process
  exits with code 6.
- **AC-011**: Given an unrecognized error occurs, When it reaches
  `handleError`, Then the process exits with code 1.
- **AC-012**: Given `GRAFANACTL_AGENT_MODE=0` is set alongside `CLAUDE_CODE=1`,
  When `IsAgentMode()` is called, Then it returns false.

## Negative Constraints

- **NC-001**: Agent mode MUST NOT auto-approve destructive operations in this
  phase. The `AutoApprove` field in `CLIOptions` is unrelated and MUST NOT
  be modified.
- **NC-002**: The exit code taxonomy MUST NOT use codes >= 128 (reserved for
  signal-killed processes in POSIX). Codes 2 and 4 MUST NOT be assigned new
  meanings; they are reserved by `design-guide.md` Section 2.1.
- **NC-003**: Agent mode detection MUST NOT perform network calls or file I/O.
  It reads only env vars and CLI args.
- **NC-004**: The `internal/agent` package MUST NOT import any `cmd/` packages
  (dependency direction: cmd imports internal, never the reverse).

## Risks

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Pre-parsing `os.Args` for `--agent` is fragile if cobra changes arg handling | Low | Medium | Pre-parse uses simple linear scan; does not attempt full flag parsing. Unit tests cover edge cases (flag after `--`, combined short flags). |
| Future env var collisions (e.g., `CLAUDE_CODE` means something else) | Low | Low | Each var is checked for truthy values only; the explicit `GRAFANACTL_AGENT_MODE` serves as the canonical escape hatch. |
| Exit code 2 conflicts with bash builtins (misuse of shell builtin) | Low | Low | Code 2 is only for cancellation in our taxonomy; agents consuming exit codes are not bash builtins. Document the taxonomy clearly. |

## Open Questions

| ID | Question | Status |
|----|----------|--------|
| OQ-1 | Should agent mode be surfaced in `grafanactl config check` output? | Deferred to Phase 4. |
| OQ-2 | Should the version-incompatible exit code (4) be emitted from `config check` today or only from API-calling commands? | Implement where version checks already exist; extend in later phases. |
| OQ-3 | Should `GRAFANACTL_AGENT_MODE=0` explicitly disable agent mode even if `CLAUDE_CODE=1` is set? | RESOLVED: Yes -- explicit opt-out in FR-001. Implement as part of T1. |
