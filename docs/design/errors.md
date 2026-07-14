# Error Design

> Describes the DetailedError structure, how to write good suggestions, how to add error converters, and in-band JSON error reporting for agent mode.

---

## 4. Error Design

### 4.1 DetailedError Structure

All errors rendered to users pass through `DetailedError`:

```go
type DetailedError struct {
    Summary     string      // Required — one-liner describing what went wrong
    Details     string      // Optional — additional context
    Parent      error       // Optional — underlying error
    Suggestions []string    // Optional — actionable fixes
    DocsLink    string      // Optional — link to documentation
    ExitCode    *int        // Optional — override exit code (default: 1)
}
```

Rendering format (stderr, colored):
```
Error: File not found
│
│ could not read './dashboards/foo.yaml'
│
├─ Suggestions:
│
│ • Check for typos in the command's arguments
│
└─
```

Reference: `cmd/gcx/fail/detailed.go`

### 4.2 Writing Good Suggestions

Every `DetailedError` **should** include at least one actionable suggestion.
Suggestions must be commands the user can run — not vague advice:

```go
// Good:
Suggestions: []string{
    "Review your configuration: gcx config view",
    "Set your token: gcx config set contexts.<ctx>.grafana.token <value>",
}

// Bad:
Suggestions: []string{
    "Check your configuration",
    "Make sure things are set up correctly",
}
```

### 4.3 Error Converter Extension

Add new error types by implementing a converter function and appending to
`errorConverters` in `cmd/gcx/fail/convert.go`:

```go
func convertMyErrors(err error) (*DetailedError, bool) {
    var myErr *mypackage.SpecificError
    if !errors.As(err, &myErr) {
        return nil, false
    }
    return &DetailedError{
        Summary:     "Descriptive summary",
        Parent:      err,
        Suggestions: []string{"gcx ..."},
    }, true
}
```

Converters are tried in order — first match wins. Place more specific
converters before more general ones.

#### Fleet Management HTTP errors

`gcx fleet` and `gcx instrumentation` reach Fleet Management through the
`grafana-collector-app` plugin proxy (ADR-021), so 401/403 responses are Grafana
auth/RBAC outcomes, not Cloud-token problems. They are handled by two converters
in `cmd/gcx/fail/convert.go`, both ordered before the generic fallback:
`convertFleetHTTPErrors` (typed `fleet.HTTPError`, from the instrumentation
client) and the string-matched `fleet:` branches (from the fleet provider's
`status <code>` errors).

- HTTP 401 → summary: `"Authentication failed"` — the Grafana credential was
  rejected; suggestion: re-authenticate with `gcx login`.
- HTTP 403 → summary: `"Authorization failed"` — insufficient Grafana role;
  suggestions name the required role (Viewer for reads, Grafana Admin for
  mutations) and note the outcome is distinguishable from an FM request
  rejection.

Both produce `DetailedError` with the `ExitAuthFailure` exit code. Neither
suggests a Cloud access-policy token — fleet/instrumentation no longer use one.

### 4.4 In-Band Error Reporting

When agent mode is active and a command fails, a JSON error object is written
to **stdout** in addition to the existing stderr `DetailedError` output
(NC-003 — in-band JSON is additive, not a replacement).

**Error-only response** (command fails completely):

```json
{"error": {"summary": "Resource not found - code 404", "exitCode": 1}}
```

**Partial failure** (batch operation, some resources succeeded):

```json
{
  "items": [...],
  "error": {"summary": "3 resources failed", "exitCode": 4, "details": "...", "suggestions": ["..."]}
}
```

**JSON schema** (`error` object):

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `summary` | string | yes | One-liner from `DetailedError.Summary` |
| `exitCode` | int | yes | Matches the process exit code |
| `details` | string | no | Omitted when empty |
| `suggestions` | []string | no | Omitted when empty |
| `docsLink` | string | no | Omitted when empty |

**Guarantees:**
- On success, no `error` key appears in stdout JSON (NC-004).
- When agent mode is NOT active, no error JSON is written to stdout.
- The JSON is always valid — partial writes cannot corrupt it (NC-004).

**Implementation:** `cmd/gcx/fail/json.go` (`DetailedError.WriteJSON`).
Invoked from `handleError` in `cmd/gcx/main.go` when `agent.IsAgentMode()` is true.

See [agent-mode.md](agent-mode.md) for the full agent mode specification.
See [exit-codes.md](exit-codes.md) for exit code values referenced in `exitCode` fields.

---

## Summary vocabulary

Error summaries in `cmd/gcx/fail/` MUST be drawn from the following vocabulary.
Adding a new summary requires a PR amending this list.

| Summary | When to use |
|---|---|
| `Invalid command usage` | Wrong flags, conflicting flags, missing required args |
| `Invalid configuration` | Bad config file, unresolvable context |
| `Authentication failed` | Token expired or missing |
| `Authorization failed` | Permission denied (403) |
| `Resource not found` | 404 or client-side not-found detection |
| `Resource conflict` | Optimistic lock / RMW conflict |
| `Network error` | Connection refused, DNS failure |
| `API error` | Non-404/403 HTTP error from backend |
| `Unexpected error` | Catch-all — no typed converter matched |

Converters in `cmd/gcx/fail/convert.go` MUST set `Summary` to a value from this table.
The `fallbackDetailedError` path sets `Unexpected error` only when no typed converter matches.
