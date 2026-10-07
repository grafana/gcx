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
  flags, unknown commands, and missing required flags. Instrumentation setup's
  mutually exclusive flag pairs also exit 2 through
  `convertInstrumentationMutualExclusiveErrors`.
- Exit code 3 (auth failure) covers HTTP 401/403 in the K8s, query, datasource,
  service, Fleet, GCOM, and login converters. A 401 reports `Authentication failed`
  and suggests credential recovery; a 403 reports `Authorization failed` and
  suggests checking roles or scopes. Adaptive Logs `invalid scope` reports
  authorization failure for both statuses.
- Missing Cloud credentials, and a missing Synthetic Monitoring token whose
  auto-discovery cannot start without Cloud credentials or stack configuration,
  exit 3. Synthetic Monitoring register/install permission failures also exit 3,
  including HTTP 400 responses that explicitly report insufficient permissions.
  Missing non-credential settings such as SM URL or a Cloud stack slug remain
  configuration failures with exit 1 when they do not prevent token discovery.
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

Malformed resource selector syntax exits 2. A well-formed selector whose resource
type or API version is not served by the current server exits 1 with
`Endpoint not available`; it does not imply incorrect command syntax.

### 2.2 Setting Exit Codes in Converters

When writing or modifying error converters in `cmd/gcx/fail/convert.go`,
set the `ExitCode` field on `DetailedError`:

```go
// In convertAPIErrors, for auth failures:
exitCode := 3
return &DetailedError{
    Summary:  "Authorization failed", // HTTP 403 (401: "Authentication failed")
    ExitCode: &exitCode,
    Suggestions: []string{...},
}, true
```

For partial failures, the command itself should set exit code 4 when
`OperationSummary.FailedCount() > 0`.

### 2.3 Cobra Usage Errors

Cobra flag parsing and positional argument validation errors are wrapped in
`UsageError` by the root command and exit with code 2. They use the summary
`Invalid command usage`, include the command's expected usage, and suggest
running that command with `--help`. Unknown commands and missing required
flags also exit with code 2. `SilenceUsage: true` keeps Cobra from printing a
second usage message alongside the structured error.

Reference: `cmd/gcx/main.go`, `internal/gcxerrors/detailed.go`,
`cmd/gcx/fail/convert.go`

See also [errors.md](errors.md) for the `DetailedError` structure and converter pattern.
See [environment-variables.md](environment-variables.md) for exit-code-related help topics.
