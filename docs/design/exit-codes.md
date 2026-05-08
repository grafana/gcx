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
| 5 | `ExitCancelled` | Cancelled | User pressed Ctrl+C (SIGINT) or `context.Canceled` |
| 6 | `ExitVersionIncompatible` | Version incompatible | Grafana version < 12 detected |

Constants defined in `cmd/gcx/fail/exitcodes.go`.

**Implementation state:**
- Exit code 2 (usage error) is set by `convertUsageErrors`,
  `convertCobraUnknownCommandErrors`, and `convertRequiredFlagErrors` for bad
  flags, unknown commands, and missing required flags.
- Exit code 3 (auth failure) is set by `convertAPIErrors` for HTTP 401/403.
- Exit code 4 (partial failure) is set by `convertPartialFailureErrors` when
  push, pull, delete, or validate operations have mixed success/failure results.
  Commands return a `PartialFailureError` when `--on-error=fail` (default) and
  `FailedCount > 0`.
- Exit code 5 (cancelled) is set by `convertContextCanceled` (first in converter
  chain) and by a fast-path check in `handleError` for `context.Canceled`.
- SIGINT is handled via `signal.NotifyContext` in `main.go`, which cancels the
  context and produces exit code 5.
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
