---
type: feature-tasks
title: "Agent Mode Foundation"
status: approved
spec: spec/feature-agent-mode-foundation/spec.md
plan: spec/feature-agent-mode-foundation/plan.md
created: 2026-03-06
---

# Implementation Tasks

## Dependency Graph

```
T1 (agent package)  ----+
                        +---> T3 (wiring) ---> T4 (tests + docs)
T2 (exit codes)     ----+
```

T1 and T2 are independent (Wave 1, parallel). T3 depends on T1 (imports
`internal/agent`). T4 depends on T3 (tests require full wiring).

## Wave 1: Independent Foundation (parallel)

### T1: Create `internal/agent` Package

**Priority**: P1
**Effort**: Small
**Depends on**: None
**Type**: task

Create the `internal/agent` package that provides agent mode detection logic.
The package reads environment variables at `init()` time and exposes `IsAgentMode()`
and `SetFlag()` for the rest of the codebase.

Detection rules (in priority order):
1. If `GCX_AGENT_MODE` is set to a falsy value (`0`, `false`, `no`) → **disabled** (overrides all others)
2. If any of `GCX_AGENT_MODE`, `CLAUDE_CODE`, `CURSOR_AGENT`, `GITHUB_COPILOT`, `AMAZON_Q` is truthy → **enabled**
3. If `SetFlag(true)` has been called → **enabled**
4. Otherwise → **disabled**

**Deliverables:**
- `internal/agent/agent.go` — new file with `init()`, `IsAgentMode() bool`, `SetFlag(bool)`, `DetectedFromEnv() bool`, `isTruthy(string) bool`
- `internal/agent/agent_test.go` — table-driven tests for all detection scenarios

**Acceptance criteria:**
- GIVEN no agent-related env vars are set, WHEN `IsAgentMode()` is called, THEN it returns `false`
- GIVEN `CLAUDE_CODE=1` is set, WHEN `IsAgentMode()` is called, THEN it returns `true`
- GIVEN `GCX_AGENT_MODE=0` and `CLAUDE_CODE=1` are both set, WHEN `IsAgentMode()` is called, THEN it returns `false` (explicit disable wins)
- GIVEN `SetFlag(true)` has been called, WHEN `IsAgentMode()` is called, THEN it returns `true`
- GIVEN `go test ./internal/agent/...` is run, THEN all tests pass
- GIVEN the import graph is inspected, THEN `internal/agent` has zero `cmd/` imports

---

### T2: Implement Exit Code Taxonomy

**Priority**: P1
**Effort**: Medium
**Depends on**: None
**Type**: task

Define exit code constants and wire differentiated exit codes into the error
conversion pipeline. Add SIGINT handler and `context.Canceled` detection.

**Deliverables:**
- `cmd/gcx/fail/exitcodes.go` — new file with constants `ExitSuccess=0`, `ExitGeneralError=1`, `ExitUsageError=2` (reserved), `ExitAuthFailure=3`, `ExitPartialFailure=4` (reserved), `ExitCancelled=5`, `ExitVersionIncompatible=6` (reserved/planned)
- `cmd/gcx/fail/convert.go` — add `convertContextCanceled` as first converter; set `ExitCode: &ExitAuthFailure` in `convertAPIErrors` 401/403 branch; add `intPtr(int) *int` helper
- `cmd/gcx/main.go` — add `signal.NotifyContext` for SIGINT; pass context to `ExecuteContext`; add `context.Canceled` fast-path in `handleError`

**Acceptance criteria:**
- GIVEN an API call returns HTTP 401, WHEN the error flows through `ErrorToDetailedError`, THEN `DetailedError.ExitCode` points to `3`
- GIVEN an API call returns HTTP 403, WHEN the error flows through `ErrorToDetailedError`, THEN `DetailedError.ExitCode` points to `3`
- GIVEN a `context.Canceled` error, WHEN it flows through `ErrorToDetailedError`, THEN `DetailedError.ExitCode` points to `5`
- GIVEN a `context.Canceled` wrapping a 401 error, WHEN `handleError` processes it, THEN the process exits with code `5` (not `3` — cancellation wins)
- GIVEN SIGINT is sent to the process, WHEN a command is running, THEN the context is cancelled and the process exits with code `5`
- GIVEN `make lint && make tests` is run, THEN all checks pass

---

## Wave 2: Wiring (depends on T1)

### T3: Wire Agent Mode into Command Lifecycle

**Priority**: P1
**Effort**: Medium
**Depends on**: T1, T2
**Type**: task

Connect `internal/agent` to the CLI command lifecycle: pre-parse `os.Args` in
`main.go`, register `--agent` flag on root command, set `color.NoColor` in
`PersistentPreRun`, and override output format default in `io.Options.BindFlags()`.

**Deliverables:**
- `cmd/gcx/main.go` — add `os.Args` pre-parse for `--agent`/`--agent=true`/`--agent=false`; call `agent.SetFlag()`
- `cmd/gcx/root/command.go` — register `--agent` persistent flag; add agent mode color suppression to `PersistentPreRun`
- `cmd/gcx/io/format.go` — in `BindFlags()`, add `if agent.IsAgentMode() { defaultFormat = "json" }` override after `DefaultFormat()` check

**Acceptance criteria:**
- GIVEN `CLAUDE_CODE=1` is set, WHEN `gcx resources list` runs without `-o`, THEN default output format is `json`
- GIVEN `--agent` is passed, WHEN the command runs, THEN `agent.IsAgentMode()` returns `true` and output defaults to `json`
- GIVEN `CLAUDE_CODE=1` and `-o text`, WHEN the command runs, THEN output format is `text` (explicit flag wins)
- GIVEN agent mode is active, WHEN `PersistentPreRun` executes, THEN `color.NoColor` is `true`
- GIVEN `--agent=false` alongside `CLAUDE_CODE=1`, WHEN the command runs, THEN agent mode is **disabled**
- GIVEN `make build` is run, THEN compilation succeeds

---

## Wave 3: Tests and Documentation (depends on T3)

### T4: Integration Tests and Design Guide Update

**Priority**: P1
**Effort**: Medium
**Depends on**: T3
**Type**: task

Write tests covering the full wiring of agent mode and exit codes across packages.
Update `docs/reference/design-guide.md` to reflect implemented changes.

**Deliverables:**
- `cmd/gcx/fail/convert_test.go` — tests for `convertContextCanceled` (plain + wrapped), auth exit code 3, converter ordering (cancel wrapping 401 → 5 not 3)
- `cmd/gcx/io/format_test.go` — test that agent mode forces `json` default even when `DefaultFormat("text")` was called
- `docs/reference/design-guide.md` — update Section 2.1 (add codes 5+6), Section 6.1 (all 5 env vars + disable behavior + `--agent` flag)

**Acceptance criteria:**
- GIVEN `go test ./cmd/gcx/fail/...`, THEN all exit code converter tests pass
- GIVEN `go test ./cmd/gcx/io/...`, THEN agent mode format override test passes
- GIVEN the design-guide Section 2.1, THEN exit codes 5 (Cancelled) and 6 (Version incompatible) are listed
- GIVEN the design-guide Section 6.1, THEN all 5 agent detection env vars are listed with the `GCX_AGENT_MODE=0` disable behavior documented
- GIVEN `make all` is run, THEN lint, tests, build, and docs all pass with no regressions
