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

Reference: `internal/gcxerrors/detailed.go`

### 4.2 Writing Good Suggestions

Every `DetailedError` **should** include at least one actionable suggestion.
Suggestions must be commands the user can run — not vague advice:

```go
// Good:
Suggestions: []string{
    "Review your configuration: gcx config view",
    "Set your token: gcx config set stacks.<name>.grafana.token <value>",
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
func convertMyErrors(err error) (*gcxerrors.DetailedError, bool) {
    var myErr *mypackage.SpecificError
    if !errors.As(err, &myErr) {
        return nil, false
    }
    return &gcxerrors.DetailedError{
        Summary:     gcxerrors.SummaryAPIError, // a constant from the summary vocabulary below
        Details:     "My service request failed: " + myErr.Message,
        Suggestions: []string{"gcx ..."},
    }, true
}
```

Converters are tried in order — first match wins. Place more specific
converters before more general ones.

#### HTML response details

The converter chain applies one shared rendering helper to each converted error.
It detects HTML with `http.DetectContentType` and the response Content-Type when
available, then replaces body details with a generic explanation that includes
the HTTP status and caller context. The domain converter still decides the
summary, exit code, and remediation: an HTML 403 is authorization failure, and
an HTML 502 is an API error. Raw HTML is not rendered, and non-HTML messages
are not truncated by this helper. Typed transport errors keep their original
`Error()` text. Raw `gcx api` is exempt and retains the complete body. Query
errors embedded in HTTP 200 also keep their existing handling.

#### Shared transport status errors

The last typed converter matches concrete `gcxerrors.HTTPStatusError` values
with `errors.As`, after domain-specific converters. HTTP 401 means
`Authentication failed` (exit 3); 403 means `Authorization failed` (exit 3);
404 means `Resource not found`; 409 means `Resource conflict`; other statuses
mean `API error` (exit 1). Parsed server messages and trace IDs appear once in
details, with caller context. Transport `Message` text and the method set stay
unchanged. `gcx api` retains its complete raw body.

Synthetic Monitoring token discovery is classified by cause: missing Cloud
credentials or stack configuration is authentication failure, register/install
permission denial is authorization failure, and service/network outages remain
API/network errors. Details identify the token discovery failure.

#### Fleet Management HTTP errors

The `convertFleetHTTPErrors` converter handles typed `fleet.HTTPError` values
from instrumentation and Fleet provider clients before the shared transport
converter and generic fallback.

- HTTP 401 → `Authentication failed`, exit 3, with credential recovery suggestions.
- HTTP 403 → `Authorization failed`, exit 3, with role/action checks.
- HTTP 404 → `Resource not found` only when the response is Connect JSON with
  `"code":"not_found"`; every other 404 means `Endpoint not available`, including
  missing plugins, proxy routes, and unknown RPC paths.
- HTTP 409 → `Resource conflict`; other statuses → `API error`, exit 1.

Pipeline and collector getters retain the typed HTTP error for both kinds of
404, including the requested resource ID in caller context. Name lookup follows
only Connect resource-not-found errors; other get failures are returned directly.
If name lookup has no match, the original typed resource failure is retained.

#### Error details and duplicate causes

Fleet and GCOM errors render caller operation, parsed server message, error code
when available, and HTTP status/trace ID once in details. Raw response text is
the fallback when no server message parses. They omit `Parent` because it would
repeat those details. Filesystem errors likewise render the full wrapped syscall
error once in details, preserving operation, path, and cause.

GCOM create/update 409 with code `InvalidArgument` remains `Invalid stack request`
(exit 2); other 409s remain `Resource conflict`, including delete protection with
its specific remediation.

### 4.4 In-Band Error Reporting

When agent mode (or `--json`) is active and a command fails, a JSON error
object is written to **stdout** and the human-formatted stderr rendering is
suppressed — machine consumers get exactly one error document, on one
stream. The stderr fallback appears only if the stdout write itself fails.
(Historical note: the original NC-003 design made in-band JSON additive to
the stderr output; the implementation intentionally converged on
either/or in `reportError`, `cmd/gcx/main.go`.)

The envelope carries collision-resistant discriminators:
`{"type": "gcx.error", "schema_version": "1", "error": {...}}`. The fused
partial-failure envelope uses `"type": "gcx.partial_result"` with `items`
alongside `error`.

**Error-only response** (command fails completely):

```json
{"type": "gcx.error", "schema_version": "1", "error": {"summary": "Resource not found", "exitCode": 1}}
```

**Partial failure** (batch operation, some resources succeeded):

```json
{
  "type": "gcx.partial_result",
  "schema_version": "1",
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
- When neither agent mode nor `--json` is active, no error JSON is written
  to stdout (an active `--json` routes the error document to stdout even on
  a TTY — machine consumers asked for machine output).
- The JSON is always valid — partial writes cannot corrupt it (NC-004).
- A command that already emitted its complete result document (including
  fused error content) returns `gcxerrors.EmittedError`; the reporter then
  writes nothing further, so stdout never carries two documents.

**Implementation:** `internal/gcxerrors/json.go` (`DetailedError.WriteJSON`),
invoked from `reportError` in `cmd/gcx/main.go` when `agent.IsAgentMode()` is
true or `--json` is active.

See [agent-mode.md](agent-mode.md) for the full agent mode specification.
See [exit-codes.md](exit-codes.md) for exit code values referenced in `exitCode` fields.

---

## Summary vocabulary

Error summaries in `cmd/gcx/fail/` MUST be drawn from the following vocabulary.
Adding a new summary requires a PR amending this list. Service, datasource,
query language, operation, version, counts, and identifiers go in `Details`,
not in the summary.

| Summary | When to use |
|---|---|
| `Invalid command usage` | Wrong flags, conflicting flags, missing required args or flags |
| `Invalid configuration` | Bad or unparseable config file, unresolvable context, missing non-credential settings (e.g. SM URL, Cloud stack slug) |
| `Authentication failed` | gcx has no credential, or the server rejected it: HTTP 401, expired or missing token, missing Cloud credentials or an SM token whose auto-discovery cannot start. Suggestions point at `gcx login` or setting a token |
| `Keychain locked` | The OS keychain answers, but it is locked or the current session cannot unlock it, so gcx cannot store or use the credential |
| `Keychain unavailable` | The OS keychain cannot be reached, so gcx cannot store or use the credential without an explicit plaintext-storage opt-out |
| `OS credential store access is restricted` | The credential store is available, but the current execution session cannot write to it |
| `Authorization failed` | The credential was accepted but lacks permission: HTTP 403, access-policy scope errors (Adaptive Logs `invalid scope` regardless of 401/403). Suggestions point at roles, access-policy scopes, or `gcx setup status`, not `gcx login` |
| `Resource not found` | 404 or client-side not-found detection |
| `Resource conflict` | Optimistic lock / RMW conflict, or an API-reported conflict whose exact cause is not machine-discriminable (e.g. GCOM stack 409s, including delete protection) |
| `Invalid stack request` | GCOM rejected stack create/update arguments (409 with code `InvalidArgument`) |
| `Invalid query` | The datasource rejected a query or query request (HTTP 400), including query-language parse errors |
| `Unsupported Grafana version` | The Grafana server is older than gcx supports (exit code 6) |
| `Partial failure` | A batch operation completed, but some resources failed (exit code 4); the counts go in details |
| `Network error` | Connection refused, DNS failure, server unreachable, timeouts |
| `API error` | Non-404/403 HTTP error from backend |
| `Endpoint not available` | The requested API route, resource type, or API version is absent on this deployment (e.g. an experimental or Cloud-only endpoint, or no Kubernetes-style `/apis`), as opposed to a missing resource instance |
| `Operation cancelled` | The user cancelled (exit code 5): Ctrl-C or another context cancellation, or Cancel on a browser login's consent page |
| `File not found` | A local file or directory does not exist |
| `Invalid path` | A local path is not valid for the operation |
| `File access denied` | The OS denied access to a local file or directory |
| `Unexpected error` | Catch-all — no typed converter matched |

Converters in `cmd/gcx/fail/` MUST set `Summary` to one of the
`gcxerrors.Summary*` constants (`internal/gcxerrors/summaries.go`), one per row
of this table. When no typed converter matches, `fallbackDetailedError` always
sets `Unexpected error` and puts the full error message in `Details`; it never
derives a summary from the message text.

Tests in `cmd/gcx/fail/` enforce this:

- A static check parses the package's non-test sources. Every `Summary:` field
  and `.Summary =` assignment must be a `gcxerrors.Summary*` constant, or a call
  to a package-level helper whose every `return` is one. String literals,
  `fmt.Sprintf`, and concatenation fail with the file and line.
- A drift check keeps the constants, `gcxerrors.Summaries()`, and this table
  identical, in both directions.
- Converter tests check that every summary they produce is in the vocabulary.
  A chain that already carries a `DetailedError` passes through unchanged and
  is not checked; summaries built outside `cmd/gcx/fail/` are not covered by
  this policy.
