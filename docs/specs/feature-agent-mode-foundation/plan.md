---
type: feature-plan
title: "Agent Mode Foundation"
status: approved
spec: spec/feature-agent-mode-foundation/spec.md
created: 2026-03-06
---

# Architecture and Design Decisions

## Pipeline Architecture

Agent mode detection must resolve **before** Cobra parses flags, because
`io.Options.BindFlags()` runs during command construction (inside `root.Command()`),
which happens before `cobra.Execute()` and therefore before `PersistentPreRun`.

```
main()
  |
  +-- (1) Pre-parse os.Args for --agent flag
  +-- (2) internal/agent.init() reads env vars (CLAUDE_CODE, CURSOR_AGENT, etc.)
  +-- (3) agent.SetFlag(true) if --agent found in os.Args
  |       agent.IsAgentMode() now returns the resolved value
  |
  +-- root.Command(version)
  |     |
  |     +-- io.Options.BindFlags()          <-- reads agent.IsAgentMode()
  |     |     if agent mode: defaultFormat = "json" (overrides command-set default)
  |     |
  |     +-- rootCmd.PersistentFlags().BoolVar(&agent, "--agent", ...)
  |     |
  |     +-- PersistentPreRun:
  |           +-- if agent.IsAgentMode(): color.NoColor = true
  |           +-- (existing) --no-color flag, logging setup
  |
  +-- signal.NotifyContext(ctx, SIGINT)     <-- wraps Execute context
  +-- rootCmd.ExecuteContext(ctx)
  |
  +-- handleError(err)
        +-- context.Canceled check -> exit code 5
        +-- fail.ErrorToDetailedError(err) -> may return exit code 3, 5, etc.
        +-- os.Exit(exitCode)
```

### Exit Code Flow

```
RunE returns error
       |
   handleError(err)
       |
   +-- Is err == context.Canceled or wraps it?
   |     YES -> exit 5
   |
   +-- fail.ErrorToDetailedError(err)
   |     |
   |     +-- converter chain (order matters):
   |     |     convertContextCanceled   <-- NEW: exit 5 (first, to catch wrapped cancels)
   |     |     convertConfigErrors
   |     |     convertFSErrors
   |     |     convertResourcesErrors
   |     |     convertNetworkErrors
   |     |     convertAPIErrors         <-- MODIFIED: exit 3 on 401/403
   |     |     convertLinterErrors
   |     |
   |     +-- fallback: exit 1
   |
   +-- os.Exit(exitCode)
```

## Design Decisions

| # | Decision | Rationale |
|---|----------|-----------|
| D1 | `internal/agent` package with no `cmd/` imports | Keeps detection logic importable by both `cmd/grafanactl/io` (for BindFlags) and `cmd/grafanactl/root` (for flag registration). Avoids circular dependency. |
| D2 | init()-time env var detection via package-level variable | `io.Options.BindFlags()` is called during command construction (before Execute). Detection must be available before any cobra lifecycle hook runs. Using `init()` ensures the env state is captured at process start. |
| D3 | Pre-parse `os.Args` for `--agent` in `main()` before `root.Command()` | The `--agent` persistent flag is registered by cobra, but its value is not parsed until `Execute()`. We need the flag's effect during command construction. A simple `os.Args` scan in `main()` resolves this chicken-and-egg problem. |
| D4 | `GRAFANACTL_AGENT_MODE=0` as explicit disable | Allows users to override auto-detection when running inside an agent environment (e.g., testing grafanactl behavior in human mode from within Claude Code). Without an explicit disable, there would be no escape hatch. |
| D5 | Exit code constants in `cmd/grafanactl/fail/exitcodes.go` | Centralizes the taxonomy in one file. Constants are typed `int` because `DetailedError.ExitCode` is `*int` and Cobra expects plain exit codes. Using the `fail` package keeps exit codes co-located with error conversion. |
| D6 | SIGINT handler in `main()` using `signal.NotifyContext` | Standard Go pattern. When SIGINT fires, the context is cancelled, and in-flight operations that respect context (errgroup, HTTP clients) wind down. `handleError` checks for `context.Canceled` and exits with code 5. |
| D7 | `convertContextCanceled` placed FIRST in converter chain | Context cancellation can wrap other errors. If placed later, a cancelled API call might match `convertAPIErrors` first and return exit code 1 instead of 5. First-position ensures cancellation is always correctly classified. |
| D8 | Exit code 6 (version incompatible) constant only, no converter yet | No existing code path produces a "Grafana version too old" error today. The constant is defined for forward compatibility. The design-guide documents it as `[PLANNED]`. |
| D9 | Codes 2 and 4 reserved per design-guide | Exit code 2 = usage error and exit code 4 = partial failure are reserved by `agent-docs/design-guide.md` Section 2.1. This phase skips them to avoid conflicting with the intended taxonomy. |

## Compatibility

| Concern | Impact | Mitigation |
|---------|--------|------------|
| Existing exit code 1 behavior | Unchanged. Commands that do not match new converters still exit 1. | Default fallback in `handleError` remains `exitCode := 1`. |
| `--no-color` flag | Still works. Agent mode sets `color.NoColor = true` additively. No conflict. | Both paths write to the same global. |
| `io.Options.DefaultFormat("text")` calls | When agent mode is active, `BindFlags()` forces default to `json`. Explicit `-o text` still works. | The override only affects the default; explicit flags take precedence via cobra's flag parsing. |
| `GRAFANACTL_AUTO_APPROVE` env var | Orthogonal to agent mode. No interaction. | Separate env var, separate config struct. |
| Provider commands using `io.Options` | Automatically inherit agent-mode JSON default. No per-provider changes needed. | The override is in `BindFlags()`, which all commands call. |
