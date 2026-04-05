# Error Design

> Describes the DetailedError structure, how to write good suggestions, how to add error converters, and in-band JSON error reporting for agent mode.
> Status markers: **[CURRENT]** = enforced, **[ADOPT]** = new code must follow, **[PLANNED]** = future.

---

## 4. Error Design

### 4.1 DetailedError Structure `[CURRENT]`

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

### 4.2 Writing Good Suggestions `[ADOPT]`

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

### 4.3 Error Converter Extension `[CURRENT]`

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

### 4.4 In-Band Error Reporting `[CURRENT]`

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
