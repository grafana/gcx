# HTTP Debug Logging Consolidation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make `internal/httputils` the single package for HTTP client construction, transport middleware, and debug logging. Change default log level to Error. Wire `LoggingRoundTripper` into every outbound HTTP path so `-v` surfaces 5xx/transport errors and `-vvv` shows full request/response tracing. Add `--log-http-payload` flag for full body dumps. Eliminate `http.DefaultClient` usage.

**Architecture:** Change default log level from Warn to Error for quieter CLI output. Break the `httputils → config` import cycle by narrowing transport signatures to `*tls.Config`. Unify all HTTP client construction into `httputils.NewClient(ClientOpts)` / `httputils.NewDefaultClient(ctx)` with pluggable middleware (defaults to `LoggingRoundTripper`, reads context for `--log-http-payload` to add `RequestResponseLoggingMiddleware`). Wire into all three client paths: `config/rest.go` (K8s tier via `WrapTransport`), `httputils.NewDefaultClient(ctx)` (external-API tier), and `assistant.Client` (currently bare `http.DefaultClient`).

**Tech Stack:** Go stdlib (`net/http`, `crypto/tls`), `github.com/grafana/grafana-app-sdk/logging` (context-aware slog), `k8s.io/client-go/rest` (WrapTransport hook)

---

## Background

### Import cycle today

```
internal/server  ──imports──▶  internal/httputils  ──imports──▶  internal/config
internal/config  ──imports──▶  (cannot import httputils — would cycle)
internal/providers ──imports──▶  internal/config
```

`httputils/client.go` imports `internal/config` only because `NewTransport` and `NewHTTPClient`
accept `*config.Context`. They touch exactly one field: `gCtx.Grafana.TLS.ToStdTLSConfig()`.
Narrowing the parameter to `*tls.Config` breaks the cycle without moving or inlining any code.

### HTTP client landscape (current)

| Constructor | Package | Users |
|---|---|---|
| `rest.HTTPClientFor(&cfg.Config)` | k8s client-go | ~15 callers: providers via Grafana API (slo, faro, alert, kg, sigil, oncall-internal, appo11y), query clients, dashboards, datasources, api, discovery, assistanthttp |
| `providers.ExternalHTTPClient()` | `internal/providers` | 7 callers: synth, oncall, k6, fleet, `CloudRESTConfig.HTTPClient()` fallback |
| `http.DefaultClient` | stdlib | 4 callers in `internal/assistant/api.go` + `sse.go`, 1 dead fallback in adaptive/metrics |
| `httputils.NewHTTPClient()` | `internal/httputils` | 1 caller: `server/grafana/requests.go` |

### HTTP client landscape (target)

```
              ┌──────────────────────────────────────────┐
              │          httputils (shared HTTP)          │
              │  ──────────────────────────────────────   │
              │  Transport:                              │
              │    NewTransport(*tls.Config)              │
              │  Middleware:                              │
              │    LoggingMiddleware  (default)           │
              │    RequestResponseLoggingMiddleware       │
              │  Clients:                                │
              │    NewClient(ClientOpts)                  │
              │    NewDefaultClient(ctx)                  │
              │  Context helpers:                         │
              │    WithPayloadLogging(ctx, bool) context  │
              │    PayloadLogging(ctx) bool               │
              └───────────────┬──────────────────────────┘
                              │ imported by
          ┌───────────────────┼────────────────────┐
          ▼                   ▼                    ▼
    config/rest         providers/*,           server/
    WrapTransport       fleet, assistant       both middlewares
    + LoggingRT                                always

```

- `rest.HTTPClientFor` — still k8s client-go, gets `LoggingRoundTripper` via `WrapTransport` in `config/rest.go`
- `NewDefaultClient(ctx)` — reads context for `--log-http-payload`; default: `LoggingMiddleware` only, with flag: adds `RequestResponseLoggingMiddleware`
- `NewClient(ClientOpts{...})` — dev server uses this with both middlewares always
- No more `http.DefaultClient` anywhere in production code

### Verbosity flag → slog level mapping (after Task 1)

Root command (`cmd/gcx/root/command.go:116-120`) will start at `slog.LevelError` (8) and subtract 4 per `-v` flag:

| Flag | Threshold | Purpose |
|---|---|---|
| *(none)* | Error (8) | Silent — only actual errors |
| `-v` | Warn (4) | Problems — 5xx, transport failures, deprecation warnings |
| `-vv` | Info (0) | Notable events — informational context |
| `-vvv` | Debug (-4) | Full tracing — every HTTP request, internal state |

### Log level convention for LoggingRoundTripper

| Event | Level | Visible at |
|---|---|---|
| Outgoing request (method, URL) | Debug | `-vvv` |
| 2xx / 3xx / 4xx response | Debug | `-vvv` |
| 5xx response | Warn | `-v` |
| Transport error (connection refused, timeout, TLS) | Warn | `-v` |

### RequestResponseLoggingRoundTripper

Logs full `httputil.DumpRequest` / `httputil.DumpResponse` output at Debug level.
Includes headers (including `Authorization`) and body. Enabled by `--log-http-payload` flag.

---

## Task 1: Change default log level to Error

**Files:**
- Modify: `cmd/gcx/root/command.go:117`

**Step 1: Change the default**

```go
// Before
logLevel.Set(slog.LevelWarn)

// After
logLevel.Set(slog.LevelError)
```

This is a breaking change. ~78 existing Warn-level logs (e.g. "falling back to auto-discovery",
"could not save datasource UID", "GCOM URL uses http://") will no longer appear by default.
Users add `-v` to see them. This is intentional — a CLI tool should be quiet by default.

**Step 2: Run tests**

```bash
go test ./cmd/gcx/...
```
Expected: all pass (tests don't depend on log output).

**Step 3: Commit**

```bash
git add cmd/gcx/root/command.go
git commit -m "feat!: change default log level from Warn to Error

BREAKING: Default output now only shows errors. Use -v for warnings
(5xx, transport failures), -vv for info, -vvv for debug tracing.
This makes the CLI quiet by default — no unsolicited warnings."
```

---

## Task 2: Refactor `httputils` — break cycle, unify client construction, add middleware

**Files:**
- Rewrite: `internal/httputils/client.go`
- Modify: `internal/httputils/logger.go`
- Create: `internal/httputils/context.go`
- Create: `internal/httputils/client_test.go`
- Create: `internal/httputils/debug_transport_test.go`
- Modify: `internal/server/server.go:68`
- Modify: `internal/server/grafana/requests.go:31`

This task combines breaking the import cycle, adding `LoggingRoundTripper`, adding context
helpers for payload logging, and unifying client construction.

### Step 1: Write the tests

Create `internal/httputils/debug_transport_test.go`:

```go
package httputils_test

import (
    "errors"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/grafana/gcx/internal/httputils"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLoggingRoundTripper_Success(t *testing.T) {
    base := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
        return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
    })
    rt := &httputils.LoggingRoundTripper{Base: base}
    req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)

    resp, err := rt.RoundTrip(req)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if resp.StatusCode != http.StatusOK {
        t.Fatalf("expected 200, got %d", resp.StatusCode)
    }
}

func TestLoggingRoundTripper_TransportError(t *testing.T) {
    wantErr := errors.New("connection refused")
    base := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
        return nil, wantErr
    })
    rt := &httputils.LoggingRoundTripper{Base: base}
    req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)

    _, err := rt.RoundTrip(req)
    if !errors.Is(err, wantErr) {
        t.Fatalf("expected %v, got %v", wantErr, err)
    }
}

func TestLoggingRoundTripper_5xx(t *testing.T) {
    base := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
        return &http.Response{StatusCode: http.StatusBadGateway, Body: http.NoBody}, nil
    })
    rt := &httputils.LoggingRoundTripper{Base: base}
    req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)

    resp, err := rt.RoundTrip(req)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if resp.StatusCode != http.StatusBadGateway {
        t.Fatalf("expected 502, got %d", resp.StatusCode)
    }
}
```

Create `internal/httputils/client_test.go`:

```go
package httputils_test

import (
    "context"
    "testing"
    "time"

    "github.com/grafana/gcx/internal/httputils"
)

func TestNewDefaultClient_HasLoggingTransport(t *testing.T) {
    client := httputils.NewDefaultClient(context.Background())
    if _, ok := client.Transport.(*httputils.LoggingRoundTripper); !ok {
        t.Fatalf("expected Transport to be *httputils.LoggingRoundTripper, got %T", client.Transport)
    }
}

func TestNewDefaultClient_WithPayloadLogging(t *testing.T) {
    ctx := httputils.WithPayloadLogging(context.Background(), true)
    client := httputils.NewDefaultClient(ctx)
    // Outermost middleware is RequestResponseLoggingRoundTripper
    if _, ok := client.Transport.(*httputils.RequestResponseLoggingRoundTripper); !ok {
        t.Fatalf("expected outermost Transport to be *httputils.RequestResponseLoggingRoundTripper, got %T", client.Transport)
    }
}

func TestNewClient_CustomMiddleware(t *testing.T) {
    client := httputils.NewClient(httputils.ClientOpts{
        Middlewares: []httputils.Middleware{httputils.RequestResponseLoggingMiddleware},
    })
    if _, ok := client.Transport.(*httputils.RequestResponseLoggingRoundTripper); !ok {
        t.Fatalf("expected Transport to be *httputils.RequestResponseLoggingRoundTripper, got %T", client.Transport)
    }
}

func TestNewClient_DefaultTimeout(t *testing.T) {
    client := httputils.NewDefaultClient(context.Background())
    if client.Timeout != 60*time.Second {
        t.Fatalf("expected 60s timeout, got %v", client.Timeout)
    }
}

func TestNewClient_CustomTimeout(t *testing.T) {
    client := httputils.NewClient(httputils.ClientOpts{
        Timeout: 10 * time.Second,
    })
    if client.Timeout != 10*time.Second {
        t.Fatalf("expected 10s timeout, got %v", client.Timeout)
    }
}
```

### Step 2: Run tests to confirm they fail

```bash
go test ./internal/httputils/... -v
```
Expected: FAIL — new types/functions undefined.

### Step 3: Rewrite `internal/httputils/client.go`

```go
package httputils

import (
    "context"
    "crypto/tls"
    "net/http"
    "time"
)

// Middleware wraps an http.RoundTripper, e.g. for logging or tracing.
type Middleware func(http.RoundTripper) http.RoundTripper

// LoggingMiddleware wraps a transport with LoggingRoundTripper (method, URL, status).
func LoggingMiddleware(rt http.RoundTripper) http.RoundTripper {
    return &LoggingRoundTripper{Base: rt}
}

// RequestResponseLoggingMiddleware wraps a transport with RequestResponseLoggingRoundTripper
// (full request/response body dump via httputil.DumpRequest/DumpResponse).
func RequestResponseLoggingMiddleware(rt http.RoundTripper) http.RoundTripper {
    return &RequestResponseLoggingRoundTripper{DecoratedTransport: rt}
}

// ClientOpts configures NewClient. See NewDefaultClient for common usage.
type ClientOpts struct {
    TLSConfig   *tls.Config
    Timeout     time.Duration // default: 60s
    Middlewares []Middleware  // default: []Middleware{LoggingMiddleware}
}

// NewClient returns a configured *http.Client.
// Middlewares are applied in order, wrapping the base transport.
func NewClient(opts ClientOpts) *http.Client {
    timeout := opts.Timeout
    if timeout == 0 {
        timeout = 60 * time.Second
    }
    middlewares := opts.Middlewares
    if middlewares == nil {
        middlewares = []Middleware{LoggingMiddleware}
    }

    var rt http.RoundTripper = NewTransport(opts.TLSConfig)
    for _, mw := range middlewares {
        rt = mw(rt)
    }
    return &http.Client{Timeout: timeout, Transport: rt}
}

// NewDefaultClient returns an *http.Client with LoggingRoundTripper, 60s timeout,
// and default TLS settings. It does NOT carry Grafana bearer tokens — callers
// must set auth headers per request.
//
// Reads context for configuration:
//   - PayloadLogging(ctx): when true, adds RequestResponseLoggingMiddleware for full
//     request/response body dumps (includes headers — may expose tokens).
func NewDefaultClient(ctx context.Context) *http.Client {
    if PayloadLogging(ctx) {
        return NewClient(ClientOpts{
            Middlewares: []Middleware{LoggingMiddleware, RequestResponseLoggingMiddleware},
        })
    }
    return NewClient(ClientOpts{})
}

// NewTransport returns an *http.Transport with sensible defaults.
// If tlsConfig is nil, a default TLS config (TLS 1.2+, verify enabled) is used.
func NewTransport(tlsConfig *tls.Config) *http.Transport {
    if tlsConfig == nil {
        tlsConfig = &tls.Config{InsecureSkipVerify: false, MinVersion: tls.VersionTLS12}
    }
    return &http.Transport{
        Proxy:                 http.ProxyFromEnvironment,
        ForceAttemptHTTP2:     true,
        MaxIdleConns:          100,
        MaxIdleConnsPerHost:   10,
        IdleConnTimeout:       90 * time.Second,
        TLSHandshakeTimeout:  10 * time.Second,
        ExpectContinueTimeout: 1 * time.Second,
        TLSClientConfig:      tlsConfig,
    }
}
```

### Step 4: Create `internal/httputils/context.go`

```go
package httputils

import "context"

type payloadLoggingKey struct{}

// WithPayloadLogging returns a context that carries the --log-http-payload flag value.
func WithPayloadLogging(ctx context.Context, enabled bool) context.Context {
    return context.WithValue(ctx, payloadLoggingKey{}, enabled)
}

// PayloadLogging returns the --log-http-payload flag value from the context.
func PayloadLogging(ctx context.Context) bool {
    v, _ := ctx.Value(payloadLoggingKey{}).(bool)
    return v
}
```

### Step 5: Add `LoggingRoundTripper` to `internal/httputils/logger.go`

Append after the existing `RequestResponseLoggingRoundTripper` (renamed from `LoggedHTTPRoundTripper`):

```go
// LoggingRoundTripper logs HTTP method, URL, and response status at appropriate levels.
//
// Successful responses (2xx/3xx) and client errors (4xx) are logged at Debug,
// visible with -vvv. Server errors (5xx) and transport failures are logged at
// Warn, visible with -v.
type LoggingRoundTripper struct {
    Base http.RoundTripper
}

func (t *LoggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
    logger := logging.FromContext(req.Context())
    logger.Debug("http request", "method", req.Method, "url", req.URL.String())

    resp, err := t.Base.RoundTrip(req)
    if err != nil {
        logger.Warn("http error", "method", req.Method, "url", req.URL.String(), "error", err)
        return nil, err
    }

    if resp.StatusCode >= 500 {
        logger.Warn("http response", "method", req.Method, "url", req.URL.String(), "status", resp.StatusCode)
    } else {
        logger.Debug("http response", "method", req.Method, "url", req.URL.String(), "status", resp.StatusCode)
    }
    return resp, nil
}
```

Also rename the existing `LoggedHTTPRoundTripper` to `RequestResponseLoggingRoundTripper`
and update its `DecoratedTransport` field (keep the field name to minimize churn, or rename
to `Base` for consistency — implementer's choice).

### Step 6: Update caller in `server.go:68`

```go
// Before
Transport: httputils.NewTransport(s.context),

// After — extract TLS config at call site
var tlsCfg *tls.Config
if s.context.Grafana != nil && s.context.Grafana.TLS != nil {
    tlsCfg = s.context.Grafana.TLS.ToStdTLSConfig()
}
// ...
Transport: httputils.NewTransport(tlsCfg),
```

Add `"crypto/tls"` to `server.go` imports if not present.

### Step 7: Update caller in `requests.go:31`

The dev server always uses both middlewares (full body dump for dev debugging):

```go
// Before
client, err := httputils.NewHTTPClient(cfg)
if err != nil {
    httputils.Error(r, w, http.StatusText(http.StatusInternalServerError), err, http.StatusInternalServerError)
    return
}

// After
var tlsCfg *tls.Config
if cfg.Grafana != nil && cfg.Grafana.TLS != nil {
    tlsCfg = cfg.Grafana.TLS.ToStdTLSConfig()
}
client := httputils.NewClient(httputils.ClientOpts{
    TLSConfig:   tlsCfg,
    Timeout:     10 * time.Second,
    Middlewares: []httputils.Middleware{httputils.LoggingMiddleware, httputils.RequestResponseLoggingMiddleware},
})
```

Add `"crypto/tls"` and `"time"` to `requests.go` imports if not present.

### Step 8: Verify cycle is broken, run tests

```bash
go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./internal/httputils/... | grep "internal/config"
```
Expected: no output.

```bash
go test ./internal/httputils/... ./internal/server/... -v
```
Expected: all pass.

### Step 9: Commit

```bash
git add internal/httputils/ internal/server/server.go internal/server/grafana/requests.go
git commit -m "feat(httputils): unify HTTP client construction, add LoggingRoundTripper

Break the httputils → config import cycle by narrowing NewTransport to
accept *tls.Config instead of *config.Context.

Add NewClient(ClientOpts) with pluggable middleware (defaults to
LoggingRoundTripper) and NewDefaultClient(ctx) for common usage.

LoggingRoundTripper logs method+URL at Debug (-vvv) for all requests.
5xx and transport errors are logged at Warn (-v).

Rename LoggedHTTPRoundTripper to RequestResponseLoggingRoundTripper.
Add context helpers for --log-http-payload flag threading.

Server uses NewClient with both middlewares (full body dump for dev proxy)."
```

---

## Task 3: Add `--log-http-payload` global flag

**Files:**
- Modify: `cmd/gcx/root/command.go`

### Step 1: Add the persistent flag

In the flag registration block (around line 192):

```go
var logHTTPPayload bool
rootCmd.PersistentFlags().BoolVar(&logHTTPPayload, "log-http-payload", false,
    "Log full HTTP request/response bodies (includes headers — may expose tokens)")
```

### Step 2: Thread it into context

In `PersistentPreRun`, after `cmd.SetContext(ctx)` (around line 141):

```go
if logHTTPPayload {
    ctx = httputils.WithPayloadLogging(ctx, true)
    cmd.SetContext(ctx)
}
```

Add `"github.com/grafana/gcx/internal/httputils"` to imports.

### Step 3: Run tests

```bash
go test ./cmd/gcx/...
```
Expected: all pass.

### Step 4: Commit

```bash
git add cmd/gcx/root/command.go
git commit -m "feat: add --log-http-payload global flag

Threads the flag value into context via httputils.WithPayloadLogging.
NewDefaultClient(ctx) reads it automatically. When enabled, full
request/response body dumps are logged at Debug level (includes
headers — may expose tokens)."
```

---

## Task 4: Move `ExternalHTTPClient` callers to `httputils.NewDefaultClient`

**Files:**
- Delete: `internal/providers/httpclient.go`
- Modify: `internal/providers/configloader.go:41`
- Modify: `internal/providers/synth/checks/client.go:33`
- Modify: `internal/providers/synth/checks/status.go:802`
- Modify: `internal/providers/synth/probes/client.go:29`
- Modify: `internal/providers/k6/client.go:49`
- Modify: `internal/providers/k6/resource_adapter.go:80`
- Modify: `internal/providers/oncall/client.go:52`
- Modify: `internal/fleet/client.go:31`

### Step 1: Update all 8 call sites

Every `providers.ExternalHTTPClient()` becomes `httputils.NewDefaultClient(ctx)`.

Check each call site for `ctx` availability. Most provider constructors and command `RunE`
functions already have `ctx context.Context` as a parameter. For the few that don't (e.g.,
struct literal init in `NewClient`), pass `context.Background()` — honest about lacking
the command context, but still gets the right defaults.

| File | Before | After |
|---|---|---|
| `providers/configloader.go:41` | `return ExternalHTTPClient(), nil` | `return httputils.NewDefaultClient(ctx), nil` |
| `providers/synth/checks/client.go:33` | `httpClient: providers.ExternalHTTPClient()` | `httpClient: httputils.NewDefaultClient(context.Background())` |
| `providers/synth/checks/status.go:802` | `resp, err := providers.ExternalHTTPClient().Do(req)` | `client := httputils.NewDefaultClient(ctx)` + `resp, err := client.Do(req)` |
| `providers/synth/probes/client.go:29` | `httpClient: providers.ExternalHTTPClient()` | `httpClient: httputils.NewDefaultClient(context.Background())` |
| `providers/k6/client.go:49` | `httpClient = providers.ExternalHTTPClient()` | `httpClient = httputils.NewDefaultClient(context.Background())` |
| `providers/k6/resource_adapter.go:80` | `httpClient := providers.ExternalHTTPClient()` | `httpClient := httputils.NewDefaultClient(ctx)` |
| `providers/oncall/client.go:52` | `httpClient := providers.ExternalHTTPClient()` | `httpClient := httputils.NewDefaultClient(context.Background())` |
| `fleet/client.go:31` | `httpClient = providers.ExternalHTTPClient()` | `httpClient = httputils.NewDefaultClient(context.Background())` |

Note: Call sites using `context.Background()` don't get `--log-http-payload`. The implementer
should check each call site — if the function has access to `ctx` (e.g., via a cobra command
or function parameter), prefer passing that instead. The table above is a starting point;
the implementer should use the real `ctx` wherever available.

Update import blocks: add `"github.com/grafana/gcx/internal/httputils"`, remove
`"github.com/grafana/gcx/internal/providers"` where it was only used for `ExternalHTTPClient`.

### Step 2: Delete `internal/providers/httpclient.go`

```bash
rm internal/providers/httpclient.go
```

### Step 3: Verify no dangling references

```bash
grep -rn "ExternalHTTPClient" --include="*.go" . | grep -v vendor/
```
Expected: no output.

### Step 4: Build and test

```bash
go build ./... && go test ./internal/providers/... ./internal/fleet/...
```
Expected: all pass.

### Step 5: Commit

```bash
git add internal/httputils/client.go internal/providers/ internal/fleet/client.go
git commit -m "refactor: migrate ExternalHTTPClient callers to httputils.NewDefaultClient

Replace singleton providers.ExternalHTTPClient() with factory
httputils.NewDefaultClient(). Each caller gets its own *http.Client
with LoggingRoundTripper. Deletes providers/httpclient.go.

Eliminates fleet's import of providers (was only for ExternalHTTPClient)."
```

---

## Task 5: Replace `debugTransport` in `internal/config/rest.go`

**Files:**
- Modify: `internal/config/rest.go`
- Delete: `internal/config/export_test.go`
- Modify: `internal/config/rest_test.go`

### Step 1: Check if `logging` import can be removed from `rest.go`

```bash
grep -n "logging\." internal/config/rest.go
```

If `logging.FromContext` is only used by `debugTransport`, remove the `logging` import.

### Step 2: Update the import block in `rest.go`

Add `"github.com/grafana/gcx/internal/httputils"`, remove
`"github.com/grafana/grafana-app-sdk/logging"` if unused after step 3.

### Step 3: Replace `WrapTransport` closure

At `rest.go:224`, change:
```go
return &debugTransport{base: rt}
```
to:
```go
return &httputils.LoggingRoundTripper{Base: rt}
```

### Step 4: Delete the `debugTransport` struct and method (lines 234–251)

Remove the entire `debugTransport` type definition and `RoundTrip` method.

### Step 5: Update test assertion and delete export shim

Delete `internal/config/export_test.go` (the `DebugTransport = debugTransport` alias is no
longer needed since `LoggingRoundTripper` is exported from `httputils`).

In `internal/config/rest_test.go`, change:
```go
if _, ok := rt.(*config.DebugTransport); !ok {
    t.Fatalf("expected outermost transport to be *config.DebugTransport, got %T", rt)
}
```
to:
```go
if _, ok := rt.(*httputils.LoggingRoundTripper); !ok {
    t.Fatalf("expected outermost transport to be *httputils.LoggingRoundTripper, got %T", rt)
}
```

Add `"github.com/grafana/gcx/internal/httputils"` to the test imports.

### Step 6: Run tests

```bash
go test ./internal/config/... -v
```
Expected: all pass.

### Step 7: Commit

```bash
git add internal/config/rest.go internal/config/export_test.go internal/config/rest_test.go
git commit -m "refactor(config): replace local debugTransport with httputils.LoggingRoundTripper"
```

---

## Task 6: Eliminate `http.DefaultClient` from adaptive/metrics and assistant

**Files:**
- Modify: `internal/providers/metrics/adaptive/client.go:33-36`
- Modify: `internal/assistant/client.go`
- Modify: `internal/assistant/api.go`
- Modify: `internal/assistant/sse.go`

### Part A: adaptive/metrics nil fallback

The nil branch is dead code (callers always pass `signalAuth.HTTPClient` via
`CloudRESTConfig.HTTPClient()`), but `http.DefaultClient` is the wrong fallback.

In `internal/providers/metrics/adaptive/client.go`, change:

```go
// Before
// If httpClient is nil, http.DefaultClient is used.
func NewClient(baseURL string, tenantID int, apiToken string, httpClient *http.Client) *Client {
    if httpClient == nil {
        httpClient = http.DefaultClient
    }

// After
// If httpClient is nil, httputils.NewDefaultClient is used.
func NewClient(baseURL string, tenantID int, apiToken string, httpClient *http.Client) *Client {
    if httpClient == nil {
        httpClient = httputils.NewDefaultClient(context.Background())
    }
```

Add `"github.com/grafana/gcx/internal/httputils"` to imports.

### Part B: assistant package

The `assistant.Client` holds `baseURL` and `token` but no `*http.Client`. The standalone
functions in `api.go` and `sse.go` use `http.DefaultClient` directly.

**Step 1: Add `HTTPClient` to `ClientOptions` and `Client`**

In `internal/assistant/client.go`:

```go
type ClientOptions struct {
    GrafanaURL     string
    Token          string
    APIEndpoint    string
    TokenRefresher TokenRefresher
    HTTPClient     *http.Client  // NEW — if nil, httputils.NewDefaultClient(context.Background()) is used.
}

type Client struct {
    grafanaURL     string
    baseURL        string
    token          string
    logger         Logger
    tokenRefresher TokenRefresher
    httpClient     *http.Client  // NEW
}

func New(opts ClientOptions) *Client {
    // ...existing code...
    httpClient := opts.HTTPClient
    if httpClient == nil {
        httpClient = httputils.NewDefaultClient(context.Background())
    }

    return &Client{
        // ...existing fields...
        httpClient: httpClient,
    }
}
```

**Step 2: Thread `httpClient` through to standalone functions**

Add `httpClient *http.Client` parameter to `FetchChat`, `FetchChatMessages`, `CreateChat` in
`api.go`, and to `StreamChatWithApproval` / `StreamA2A` in `sse.go`. Replace every
`http.DefaultClient.Do(req)` with `httpClient.Do(req)`.

Update the calling methods in `client.go` to pass `c.httpClient`.

### Step 3: Run tests

```bash
go test ./internal/assistant/... ./internal/providers/metrics/...
```
Expected: all pass.

### Step 4: Commit

```bash
git add internal/providers/metrics/adaptive/client.go internal/assistant/
git commit -m "fix: replace http.DefaultClient with httputils.NewDefaultClient

- adaptive/metrics: use httputils.NewDefaultClient(context.Background()) for nil fallback
- assistant: add HTTPClient to ClientOptions, thread through api.go and sse.go
  so all assistant HTTP calls go through a client with LoggingRoundTripper"
```

---

## Final verification

```bash
GCX_AGENT_MODE=false make all
```

Expected: lint, tests, build, and docs all pass.

Verify the import graph is clean:

```bash
go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./internal/httputils/... | grep "internal/config"
```
Expected: no output (cycle is broken).

Verify no bare `http.DefaultClient` remains outside tests:

```bash
grep -rn "http\.DefaultClient" --include="*.go" . | grep -v "_test.go" | grep -v vendor/
```
Expected: no output.

Verify no `providers.ExternalHTTPClient` references remain:

```bash
grep -rn "ExternalHTTPClient" --include="*.go" . | grep -v vendor/
```
Expected: no output.
