# Exit Code Taxonomy

> Defines the exit codes used by gcx commands, their meanings, and how to set them in error converters.

---

## 2. Exit Code Taxonomy

### 2.1 Exit Codes

| Code | Constant | Meaning | When |
|------|----------|---------|------|
| 0 | `ExitSuccess` | Success | Command completed without errors |
| 1 | `ExitGeneralError` | General error | Unexpected error, business logic failure |
| 2 | `ExitUsageError` | Usage error | Invalid selectors, unknown commands, missing required flags (see § 2.3 for what Cobra still surfaces as 1) |
| 3 | `ExitAuthFailure` | Auth failure | 401/403, missing or invalid credentials |
| 4 | `ExitPartialFailure` | Partial failure | Some resources succeeded, others failed |
| 5 | `ExitCancelled` | Cancelled | Ctrl+C (SIGINT), `context.Canceled`, explicit confirmation cancellation (see § 2.4 for legacy exceptions), or server-reported cancellation |
| 6 | `ExitVersionIncompatible` | Version incompatible | Grafana version < 12 detected |

Constants defined in `internal/gcxerrors/exitcodes.go`.

**Implementation state:**
- Exit code 2 (usage error) is set by `convertUsageErrors`,
  `convertCobraUnknownCommandErrors`, and `convertRequiredFlagErrors` for invalid
  command input, unknown commands, and missing required flags (see § 2.3).
- Exit code 3 (auth failure) is set by `convertAPIErrors` for HTTP 401/403.
- Exit code 4 (partial failure) is set by `convertPartialFailureErrors` when
  push, pull, delete, or validate operations have mixed success/failure results.
  Commands return a `PartialFailureError` when `--on-error=fail` (default) and
  `FailedCount > 0`.
- Exit code 5 (cancelled) has several producers, and the taxonomy is about the
  final exit code rather than about any one of them. `isSilentCancellation` in
  `main.go` exits 5 without printing an error for an interrupted invocation.
  Commands that stop early after reporting their own outcome carry the same code
  themselves, and not through one error type: commands following the declined
  confirmation contract return a `DetailedError` with `ExitCode: ExitCancelled`
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
  context and produces exit code 5. A watcher goroutine calls the returned stop
  function as soon as that context is cancelled, restoring the default terminate
  action so a second Ctrl-C ends a run whose graceful shutdown has stalled — not
  from a defer, which `os.Exit` would skip. The usage export that follows is
  synchronous, so `exitWith` stands that watcher down first (`interruptGate`) and
  learns from it whether a signal had arrived; nothing else can then change the
  disposition the export runs under. `abandonsExport` is that decision, and it
  requires both that a signal arrived and that the final exit code is 5. For an
  invocation the user is abandoning the restored default action stands, so a
  second Ctrl-C ends a process still waiting on the export instead of being
  swallowed. Every other invocation holds SIGINT for the length of the export,
  including one that was never interrupted: a command that absorbs the interrupt
  and still finishes — `gcx dev serve` shuts its HTTP server down on `ctx.Done`
  and returns `nil` — must exit with the code that agrees with what it printed
  rather than dying by signal with status 130, and so must one whose first
  interrupt only arrives while the export is in flight. Deciding from a bool
  sampled before the export cannot see that second case, which is why the answer
  is read from the gate at export time. Two limits remain: `signal.Stop` restores
  the disposition the process started with, so a background job of a
  non-interactive shell (which inherits SIGINT as `SIG_IGN`) still ignores the
  second interrupt; and only SIGINT is caught at all, so a SIGTERM ends the
  process before any of this runs.
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

Cobra raises its own usage errors, and only some are mapped to exit 2. The
converters in `cmd/gcx/fail/convert.go` catch what they recognise; the rest flow
through error reporting and get exit code 1:

| Invocation | Exit | Mapped by |
|---|---:|---|
| Unknown flag (`--bogus`) | 1 | — falls through to `fallbackDetailedError` |
| Missing positional arg (`gcx api`) | 1 | — |
| Unknown command (`gcx bogus`) | 2 | `convertCobraUnknownCommandErrors` |
| Missing required flag | 2 | `convertRequiredFlagErrors` |
| `*fail.UsageError` raised by a command | 2 | `convertUsageErrors` |

Use `fail.NewCommandUsageError` for invalid input in new command code. Verify
the exit code of the actual path; normalizing existing Cobra failures is a
compatibility change, separate from adding a command or fixing guidance.

### 2.4 Declined Confirmations

New confirmation paths return `gcxerrors.DetailedError` with
`ExitCode: ExitCancelled` after `ConfirmDestructive` returns `(false, nil)`.
The helper does not assign an exit code for the caller. See the
[implementation example](safety.md#32-the-force-flag-and-providersconfirmdestructive-implemented).

**Existing compatibility gap:** a negative answer still returns `nil` (exit 0)
in `gcx datasources delete`, `gcx slo definitions delete`, `gcx slo reports delete`,
and Synth, adaptive-signal, and several other provider mutation paths. By
contrast, IRM OnCall actions and Assistant MCP-server deletion explicitly return
exit 5. Do not use an existing success-on-decline path as the template for a new
command, or change its released behavior incidentally in a guidance cleanup.
A compatibility migration must identify the affected commands and update their
output and exit-code tests together. EOF is separate: `ConfirmDestructive`
returns an error when no interactive input is available.

Reference: `cmd/gcx/main.go`, `internal/gcxerrors/detailed.go`,
`cmd/gcx/fail/convert.go`

See also [errors.md](errors.md) for the `DetailedError` structure and converter pattern.
See [environment-variables.md](environment-variables.md) for exit-code-related help topics.
