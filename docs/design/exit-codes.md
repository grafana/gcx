# Exit Code Taxonomy

> Defines the exit codes used by gcx commands, their meanings, and how to set them in error converters.

---

## 2. Exit Code Taxonomy

### 2.1 Exit Codes

| Code | Constant | Meaning | When |
|------|----------|---------|------|
| 0 | `ExitSuccess` | Success | Command completed without errors |
| 1 | `ExitGeneralError` | General error | Unexpected error, business logic failure |
| 2 | `ExitUsageError` | Usage error | Bad flags, invalid selectors, missing args |
| 3 | `ExitAuthFailure` | Auth failure | 401/403, missing or invalid credentials |
| 4 | `ExitPartialFailure` | Partial failure | Some resources succeeded, others failed |
| 5 | `ExitCancelled` | Cancelled | User pressed Ctrl+C (SIGINT), `context.Canceled`, a declined confirmation prompt, or a server-reported cancellation |
| 6 | `ExitVersionIncompatible` | Version incompatible | Grafana version < 12 detected |

Constants defined in `internal/gcxerrors/exitcodes.go`.

**Implementation state:**
- Exit code 2 (usage error) is set by `convertUsageErrors`,
  `convertCobraUnknownCommandErrors`, and `convertRequiredFlagErrors` for bad
  flags, unknown commands, and missing required flags.
- Exit code 3 (auth failure) is set by `convertAPIErrors` for HTTP 401/403.
- Exit code 4 (partial failure) is set by `convertPartialFailureErrors` when
  push, pull, delete, or validate operations have mixed success/failure results.
  Commands return a `PartialFailureError` when `--on-error=fail` (default) and
  `FailedCount > 0`.
- Exit code 5 (cancelled) has several producers, and the taxonomy is about the
  final exit code rather than about any one of them. `isSilentCancellation` in
  `main.go` exits 5 without printing an error for an interrupted invocation.
  Commands that stop early after reporting their own outcome carry the same code
  themselves, and not through one error type: a declined confirmation prompt
  returns a `DetailedError` with `ExitCode: ExitCancelled`
  (`internal/providers/irm/oncall_actions.go`,
  `internal/providers/assistant/mcpservers/commands.go`), while an aborted
  `dev scaffold` and the agent-mode assistant and instrumentation wait paths
  return an `EmittedError` carrying it. Every route exits through `exitWith`, so
  each is reported as `outcome: canceled` (see
  [anonymous usage statistics](../sources/anonymous-usage-statistics.md)), and
  the event does not distinguish them.
- `convertContextCanceled` is still first in the converter chain, but it no
  longer contributes a top-level exit 5. `isSilentCancellation` tests the same
  predicate with `errors.Is`, so any chain the converter would match — wrapped
  or not — is intercepted in `main.go` first, and a chain carrying an
  `EmittedError` returns that code from `reportError` earlier still. The
  converter remains reachable where a command converts an error itself rather
  than returning it, as `gcx config check` does.
- A SIGINT does not always arrive as `context.Canceled`: Go 1.26's
  `signal.NotifyContext` cancels with a cause describing the signal and
  `net/http` surfaces `context.Cause`, which before Go 1.26.5 did not report
  itself as `context.Canceled`. `isSilentCancellation` therefore also matches
  the invocation context's own cause. `convertContextCanceled` still tests only
  `errors.Is(err, context.Canceled)`, so cancellations classified deeper in the
  converter chain depend on the toolchain in use.
- SIGINT is handled via `signal.NotifyContext` in `main.go`, which cancels the
  context and produces exit code 5. For an invocation that was interrupted,
  `exitWith` calls the returned stop function before the synchronous usage
  export — not from a defer, which `os.Exit` would skip — so a second Ctrl-C
  ends a process still waiting on that export instead of being swallowed. Two
  limits: `signal.Stop` restores the disposition the process started with, so a
  background job of a non-interactive shell (which inherits SIGINT as
  `SIG_IGN`) still ignores the second interrupt; and the call is deliberately
  skipped when the invocation was not interrupted, so a stray SIGINT cannot
  kill a command that already finished and is only waiting out its export.
- Exit code 6 (version incompatible) is set by `convertVersionErrors` when
  Grafana version < 12 is detected.

### 2.2 Setting Exit Codes in Converters

When writing or modifying error converters in `cmd/gcx/fail/convert.go`,
set the `ExitCode` field on `DetailedError`:

```go
// In convertAPIErrors, for auth failures:
exitCode := 3
return &DetailedError{
    Summary:  fmt.Sprintf("%s - code %d", reason, code),
    ExitCode: &exitCode,
    Suggestions: []string{...},
}, true
```

For partial failures, the command itself should set exit code 4 when
`OperationSummary.FailedCount() > 0`.

### 2.3 Cobra Usage Errors

Cobra itself handles usage errors (bad flags, missing required args). With
`SilenceUsage: true` set on the root command, these errors flow through
`handleError` and get exit code 1. Future work: detect Cobra usage errors
and override to code 2.

Reference: `cmd/gcx/main.go`, `cmd/gcx/fail/detailed.go`,
`cmd/gcx/fail/convert.go`

See also [errors.md](errors.md) for the `DetailedError` structure and converter pattern.
See [environment-variables.md](environment-variables.md) for exit-code-related help topics.
