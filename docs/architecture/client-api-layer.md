# Client and API Communication Layer

## Overview

gcx has three distinct client paths to Grafana. The primary path uses the
Kubernetes dynamic client stack (k8s.io/client-go) to talk to Grafana's
Kubernetes-compatible `/apis` endpoint for resource CRUD. A secondary path uses
the Grafana OpenAPI generated client (`grafana-openapi-client-go`) for
non-resource operations like health checks and datasource listing. A third path
uses `rest.HTTPClientFor` directly to execute datasource-specific queries
(PromQL, LogQL) against Grafana's `/apis` query endpoints.

---

## Client Construction Chain

```
config.GrafanaConfig          (user-facing: server URL, auth, TLS, org/stack IDs)
        |
        v
config.NewNamespacedRESTConfig()   [internal/config/rest.go]
        |  - maps GrafanaConfig fields to k8s rest.Config
        |  - resolves namespace (stacks-N or org-N) via DiscoverStackID
        |  - sets QPS=50, Burst=100 rate limits
        v
config.NamespacedRESTConfig        (embeds rest.Config + Namespace string)
        |
        +---> dynamic.NewDefaultNamespacedClient()  [dynamic/namespaced_client.go]
        |         |  calls dynamic.NewForConfig(&cfg.Config)
        |         v
        |     dynamic.NamespacedClient     (List, Get, GetMultiple, Create, Update, Delete, Apply)
        |         ^
        |         |  wrapped by
        +---> dynamic.NewDefaultVersionedClient()   [dynamic/versioned_client.go]
                  v
              dynamic.VersionedClient     (same interface + auto-version re-fetch)
```

The secondary OpenAPI path is independent:

```
config.Context
        |
        v
grafana.ClientFromContext()     [internal/grafana/client.go]
        |  - parses server URL into Host/BasePath/Scheme
        |  - applies auth (basic or API key)
        v
*goapi.GrafanaHTTPAPI           (generated OpenAPI client, /api base path)
```

The third path for datasource queries also starts from `NamespacedRESTConfig`
but uses the k8s `rest` package's HTTP client factory directly:

```
config.NamespacedRESTConfig
        |
        v
rest.HTTPClientFor(&cfg.Config)     [k8s.io/client-go/rest]
        |  - builds *http.Client with TLS, auth, and transport from rest.Config
        |  - does NOT set up k8s API machinery (no GVK, no dynamic.Interface)
        v
*http.Client
        |
        +---> prometheus.Client     [internal/query/prometheus/client.go]
        |         |  POST /apis/query.grafana.app/v0alpha1/namespaces/{ns}/query
        |         |  GET  /apis/prometheus.datasource.grafana.app/v0alpha1/...
        |
        +---> loki.Client           [internal/query/loki/client.go]
                  |  POST /apis/query.grafana.app/v0alpha1/namespaces/{ns}/query
                  |  GET  /apis/loki.datasource.grafana.app/v0alpha1/...
```

---

## Layer Descriptions

### Layer 1: `config.GrafanaConfig` — User Configuration

**File:** `internal/config/types.go`

The root data structure that holds all user-provided connection settings:

```go
type GrafanaConfig struct {
    Server   string  // env: GRAFANA_SERVER
    User     string  // env: GRAFANA_USER
    Password string  // env: GRAFANA_PASSWORD  (datapolicy:"secret")
    APIToken string  // env: GRAFANA_TOKEN     (datapolicy:"secret")
    OrgID    int64   // env: GRAFANA_ORG_ID    (on-prem)
    StackID  int64   // env: GRAFANA_STACK_ID  (Grafana Cloud)
    TLS      *TLS    // cert/key/ca data, insecure flag, SNI
}
```

`datapolicy:"secret"` tags cause these fields to be redacted in logs. Auth
priority: APIToken beats User/Password (enforced in `NewNamespacedRESTConfig`).

### Layer 2: `NamespacedRESTConfig` — k8s REST Config Bridge

**File:** `internal/config/rest.go` — `NewNamespacedRESTConfig()`

Converts the user config into a `k8s.io/client-go/rest.Config` plus a resolved
namespace string.

**Key responsibilities:**

1. **Host mapping** — `cfg.Grafana.Server` becomes `rest.Config.Host`; `APIPath`
   is hardcoded to `"/apis"` (the K8s-compatible endpoint Grafana exposes).

2. **Auth mapping:**
   ```go
   switch {
   case cfg.Grafana.APIToken != "":
       rcfg.BearerToken = cfg.Grafana.APIToken   // → Authorization: Bearer <token>
   case cfg.Grafana.User != "":
       rcfg.Username = cfg.Grafana.User
       rcfg.Password = cfg.Grafana.Password       // → Authorization: Basic <b64>
   }
   ```

3. **TLS mapping** — gcx's `TLS` struct is manually mapped to k8s's
   `rest.TLSClientConfig` (they are incompatible types; `crypto/tls.Config` ≠
   `rest.TLSClientConfig`).

4. **Namespace resolution** — calls `DiscoverStackID()` to auto-detect Grafana
   Cloud namespace. Falls back to configured OrgID or StackID:
   ```
   DiscoverStackID succeeds  →  stacks-<discoveredID>   (cloud, auto-detected)
   DiscoverStackID fails
     OrgID configured        →  org-<OrgID>             (on-prem)
     StackID configured      →  stacks-<StackID>        (cloud, manual)
   ```

5. **Rate limits** — hardcoded `QPS: 50, Burst: 100` (TODO: make configurable).

**Stack ID auto-discovery** (`internal/config/stack_id.go`):

```
GET {server}/bootdata
→ { "settings": { "namespace": "stacks-98765" } }
→ parse "stacks-98765" → StackID = 98765
→ namespace = "stacks-98765"
```

Uses a dedicated 5-second-timeout HTTP client (separate from the main client).
If the endpoint returns non-200 or an on-prem namespace (e.g., `"grafana"`),
discovery fails and the configured values are used as-is.

### Layer 3: `NamespacedClient` — Primary CRUD Client

**File:** `internal/resources/dynamic/namespaced_client.go`

Wraps `k8s.io/client-go/dynamic.Interface` with namespace-scoped operations.
Every method scopes to `c.namespace` automatically:

```go
c.client.Resource(desc.GroupVersionResource()).Namespace(c.namespace).<op>()
```

**Operations provided:**

| Method | Notes |
|---|---|
| `List` | Uses k8s pager for automatic pagination |
| `Get` | Single resource by name |
| `GetMultiple` | Concurrent Gets via `errgroup` (no SetLimit currently) |
| `Create` | POST to resource endpoint |
| `Update` | PUT to resource endpoint |
| `Delete` | DELETE from resource endpoint |
| `Apply` | Server-side apply (PATCH with field manager) |

**Pagination pattern:**
```go
pager := pager.New(func(ctx context.Context, opts metav1.ListOptions) (runtime.Object, error) {
    return c.client.Resource(desc.GroupVersionResource()).Namespace(c.namespace).List(ctx, opts)
})
pager.EachListItemWithAlloc(ctx, opts, func(obj runtime.Object) error { ... })
```

All errors pass through `ParseStatusError()` before being returned (see Error
Translation below).

**Constructor:**
```go
// From a NamespacedRESTConfig (typical usage):
client, err := dynamic.NewDefaultNamespacedClient(cfg)

// Or inject a pre-built dynamic.Interface (e.g., for tests):
client := dynamic.NewNamespacedClient(namespace, myDynamicClient)
```

### Layer 4: `VersionedClient` — Version-Aware Client

**File:** `internal/resources/dynamic/versioned_client.go`

Embeds `*NamespacedClient` and adds automatic version re-fetching. Grafana
resources can carry a `status.conversion.storedVersion` field indicating the
actual stored API version differs from the requested version. When that field is
present, `VersionedClient` re-fetches the resource using the stored version.

**Flow for a List operation:**
```
1. Call NamespacedClient.List(ctx, desc, opts)      → initial list in requested version
2. For each item, check status.conversion.storedVersion
3. Items without storedVersion → kept as-is
4. Items with storedVersion X  → grouped by new Descriptor{version=X}
5. Call NamespacedClient.GetMultiple for each group  → re-fetch at correct version
6. Return merged list
```

Only `List`, `Get`, and `GetMultiple` are overridden. `Create`, `Update`,
`Delete`, `Apply` are inherited from `NamespacedClient` unchanged.

**When to use which client:**

| Client | Use case |
|---|---|
| `NamespacedClient` | Push operations (Create/Update/Delete/Apply) where version is known |
| `VersionedClient` | Pull operations (List/Get) where stored version may differ |

This maps directly to usage in `remote`:
- `Pusher.NewDefaultPusher()` creates a `NamespacedClient`
- `Puller.NewDefaultPuller()` creates a `VersionedClient`

---

## Error Translation

**File:** `internal/resources/dynamic/errors.go`

The k8s dynamic client returns `apierrors.StatusError` which has poor default
formatting (message can be just `"unknown"`). `ParseStatusError` wraps all
errors into `APIError` which formats as `"<code> <reason>: <message>"`:

```go
func ParseStatusError(err error) error {
    if err == nil {
        return nil
    }
    if status, ok := err.(apierrors.APIStatus); ok || errors.As(err, &status) {
        return APIError{status.Status()}
    }
    // Non-API errors become a synthetic 500 Unknown
    return APIError{
        status: metav1.Status{
            Status:  metav1.StatusFailure,
            Reason:  metav1.StatusReasonUnknown,
            Code:    http.StatusInternalServerError,
            Message: err.Error(),
        },
    }
}

func (e APIError) Error() string {
    return fmt.Sprintf("%d %s: %s", e.status.Code, e.status.Reason, e.status.Message)
}
```

`APIError` also satisfies `apierrors.APIStatus`, so callers using
`apierrors.IsNotFound(err)` and similar predicates continue to work.

**Error flow:**
```
k8s dynamic client  →  apierrors.StatusError
                              |
                    ParseStatusError()
                              |
                         APIError
                    "404 NotFound: dashboards.dashboard.grafana.app \"my-dash\" not found"
```

---

## Grafana OpenAPI Client (Secondary Path)

**File:** `internal/grafana/client.go`

The `grafana-openapi-client-go` generated client targets `/api` (not `/apis`)
and is used for Grafana-specific operations that are not part of the K8s API:

```go
func ClientFromContext(ctx *config.Context) (*goapi.GrafanaHTTPAPI, error) {
    cfg := &goapi.TransportConfig{
        Host:     grafanaURL.Host,
        BasePath: grafanaURL.Path + "/api",
        Schemes:  []string{grafanaURL.Scheme},
    }
    // Auth applied directly to TransportConfig (not rest.Config)
    if ctx.Grafana.User != "" && ctx.Grafana.Password != "" {
        cfg.BasicAuth = url.UserPassword(ctx.Grafana.User, ctx.Grafana.Password)
    }
    if ctx.Grafana.APIToken != "" {
        cfg.APIKey = ctx.Grafana.APIToken
    }
    return goapi.NewHTTPClientWithConfig(strfmt.Default, cfg), nil
}
```

**Current usages:**
- `grafana.GetVersion()` — calls `GET /api/health` to check Grafana version
- Version compatibility checks before operations
- Datasources list and get — queries the `/api/datasources` endpoint

**Does NOT use:**
- `internal/httputils` (the OpenAPI client manages its own transport)
- `NamespacedRESTConfig` (completely separate connection setup)

---

## Datasource Query Clients (Third Path)

**Packages:** `internal/query/prometheus`, `internal/query/loki`

These clients execute PromQL and LogQL queries against Grafana's datasource-
specific API endpoints. They bypass all k8s API machinery and use
`rest.HTTPClientFor` to create a plain `*http.Client` from the `rest.Config`,
then make direct HTTP requests.

**Key distinction:** Unlike `NamespacedClient` and `VersionedClient`, these
clients do not use `dynamic.Interface`, GVK resolution, or `Unstructured`
objects. They speak JSON directly to Grafana's query and datasource-proxy APIs.

### Construction

```go
// Both clients follow the same constructor pattern:
httpClient, err := rest.HTTPClientFor(&cfg.Config)   // TLS + auth from rest.Config
client := &prometheus.Client{restConfig: cfg, httpClient: httpClient}
```

The `rest.HTTPClientFor` call re-uses the same `NamespacedRESTConfig` (host,
auth, TLS) already built by `NewNamespacedRESTConfig`, so no separate auth
wiring is needed.

### API Endpoints

**Prometheus (`internal/query/prometheus/client.go`):**

| Method | HTTP | Path |
|---|---|---|
| `Query` | POST | `/apis/query.grafana.app/v0alpha1/namespaces/{ns}/query` |
| `Labels` | GET | `/apis/prometheus.datasource.grafana.app/v0alpha1/namespaces/{ns}/datasources/{uid}/resource/api/v1/labels` |
| `LabelValues` | GET | `/apis/prometheus.datasource.grafana.app/v0alpha1/namespaces/{ns}/datasources/{uid}/resource/api/v1/label/{name}/values` |
| `Metadata` | GET | `/apis/prometheus.datasource.grafana.app/v0alpha1/namespaces/{ns}/datasources/{uid}/resource/api/v1/metadata` |
| `Targets` | GET | `/apis/prometheus.datasource.grafana.app/v0alpha1/namespaces/{ns}/datasources/{uid}/resource/api/v1/targets` |

**Loki (`internal/query/loki/client.go`):**

| Method | HTTP | Path |
|---|---|---|
| `Query` | POST | `/apis/query.grafana.app/v0alpha1/namespaces/{ns}/query` |
| `Labels` | GET | `/apis/loki.datasource.grafana.app/v0alpha1/namespaces/{ns}/datasources/{uid}/resource/labels` |
| `LabelValues` | GET | `/apis/loki.datasource.grafana.app/v0alpha1/namespaces/{ns}/datasources/{uid}/resource/label/{name}/values` |
| `Series` | GET | `/apis/loki.datasource.grafana.app/v0alpha1/namespaces/{ns}/datasources/{uid}/resource/series` |

### Query Request Format

Both `Query` methods use Grafana's unified query API (not the native Prometheus
or Loki HTTP API). The request body wraps the query expression in a Grafana
data-frame envelope:

```json
{
  "queries": [{"refId": "A", "datasource": {"type": "prometheus", "uid": "<uid>"}, "expr": "<promql>"}],
  "from": "<epoch_ms or 'now-1m'>",
  "to":   "<epoch_ms or 'now'>"
}
```

**Does NOT use:**
- `dynamic.Interface`, `Unstructured`, or GVK resolution
- `ParseStatusError` / `APIError` (errors are plain Go errors with HTTP status)

---

## HTTP Utilities (`internal/httputils`)

Central HTTP client factory for all non-K8s HTTP calls. The K8s dynamic client
path uses `rest.Config` with a `WrapTransport` hook that chains
`LoggingRoundTripper`; all other callers use `httputils` directly.

### `client.go` — Client Factory

```go
func NewDefaultClient(ctx context.Context) *http.Client   // standard: LoggingRoundTripper, 60s timeout
func NewClient(opts ClientOpts) *http.Client               // custom: explicit middleware, TLS, timeout
```

`NewDefaultClient` wraps the transport with `LoggingRoundTripper` (Debug for
2xx/3xx/4xx, Warn for 5xx + transport errors). When `PayloadLogging(ctx)` is
true (set by `--log-http-payload`), it additionally wraps with
`RequestResponseLoggingMiddleware` for full request/response body dumps.

`NewClient` accepts explicit `ClientOpts` for custom middleware stacks, TLS
configuration, and timeouts. If `Middlewares` is nil, it defaults to
`[]Middleware{LoggingMiddleware}`.

### `context.go` — Payload Logging Context

```go
func WithPayloadLogging(ctx context.Context, enabled bool) context.Context
func PayloadLogging(ctx context.Context) bool
```

The `--log-http-payload` flag value is threaded into the context by root
`PersistentPreRun` and read by `NewDefaultClient`. This avoids passing flag
values through constructor chains.

### `logger.go` — Logging Round-Trippers

| Type | Log level | Content | Visible at |
|------|-----------|---------|------------|
| `LoggingRoundTripper` | Debug (2xx-4xx), Warn (5xx/error) | method, URL, status | `-vvv` / `-v` |
| `RequestResponseLoggingRoundTripper` | Debug | Full body via `httputil.Dump*` | `-vvv` + `--log-http-payload` |

### `response.go` — Server Response Helpers

Used by server-side HTTP handlers in `internal/server/handlers/`.

### `constants.go`

```go
const UserAgent = "gcx"
```

### Callers

| Caller | Factory | Notes |
|--------|---------|-------|
| Provider clients (SLO, OnCall, Synth, Fleet, K6) | `NewDefaultClient(ctx)` | Via `CloudRESTConfig.HTTPClient(ctx)` when `RESTConfig` is nil; see note below |
| Assistant client | `NewDefaultClient(ctx)` | Direct call |
| K8s tier (dynamic client, query clients) | `rest.Config.WrapTransport` | Chains `LoggingRoundTripper` via `NewNamespacedRESTConfig` |
| Dev server (`internal/server`) | `NewClient(ClientOpts{...})` | Custom TLS from config context |

> **Note on `CloudRESTConfig.HTTPClient(ctx)`:** When `RESTConfig` is nil (most
> external-API providers), it delegates to `httputils.NewDefaultClient(ctx)` and
> gets `LoggingRoundTripper`. When `RESTConfig` is non-nil, it falls back to
> `rest.HTTPClientFor()`, which does NOT go through `httputils` directly — logging
> is provided by the K8s `WrapTransport` chain instead.

---

## Authentication Flow Summary

```
GrafanaConfig.APIToken != ""
    │
    ├─ dynamic path       →  rest.Config.BearerToken
    │                        k8s transport sets "Authorization: Bearer <token>"
    │
    ├─ OpenAPI path        →  TransportConfig.APIKey
    │                        generated client sets "Authorization: Bearer <token>"
    │
    └─ query clients path →  rest.Config.BearerToken (via rest.HTTPClientFor)
                             same http.Client used by prometheus.Client / loki.Client

GrafanaConfig.User != ""
    │
    ├─ dynamic path       →  rest.Config.Username + Password
    │                        k8s transport sets "Authorization: Basic <b64(user:pass)>"
    │
    ├─ OpenAPI path        →  TransportConfig.BasicAuth
    │                        generated client sets "Authorization: Basic <b64(user:pass)>"
    │
    └─ query clients path →  rest.Config.Username + Password (via rest.HTTPClientFor)
                             same mechanism as dynamic path
```

Priority: API token always wins over basic auth (enforced by `switch` statement
in `NewNamespacedRESTConfig` and by separate `if` guards in `ClientFromContext`).

---

## Rate Limiting and Concurrency

**Per-client rate limits** (dynamic client path only):
- `rest.Config.QPS = 50` — sustained requests per second
- `rest.Config.Burst = 100` — burst capacity above QPS
- Enforced by k8s client-go's token bucket rate limiter inside the HTTP transport
- Hardcoded; not currently exposed via config or CLI flags

**Application-level concurrency** (in `NamespacedClient.GetMultiple`):
```go
g, ctx := errgroup.WithContext(ctx)
for i, name := range names {
    g.Go(func() error { ... c.Get(ctx, desc, name, opts) ... })
}
```
No `SetLimit` call — all Gets run fully concurrent (bounded only by QPS/Burst).
A TODO comment notes this should be capped.

**Push concurrency** is managed one level up in `remote.Pusher`, which does use
`errgroup.SetLimit(maxConcurrent)` with a configurable value passed from the CLI.

---

## How to Add a New API Operation

To add a new operation to the dynamic client path (e.g., `Patch`):

1. Add the method to `NamespacedClient` in `namespaced_client.go`:
   ```go
   func (c *NamespacedClient) Patch(
       ctx context.Context, desc resources.Descriptor, name string,
       pt types.PatchType, data []byte, opts metav1.PatchOptions,
   ) (*unstructured.Unstructured, error) {
       res, err := c.client.Resource(desc.GroupVersionResource()).
           Namespace(c.namespace).Patch(ctx, name, pt, data, opts)
       return res, ParseStatusError(err)
   }
   ```

2. If the operation needs version awareness, override it in `VersionedClient`
   in `versioned_client.go`. If not, it is inherited automatically.

3. Add the method to the appropriate interface in `remote/puller.go` or
   `remote/pusher.go` (`PullClient` / `PushClient`) if the operation is needed
   from a Puller/Pusher.

4. No changes needed to auth, TLS, or namespace handling — those are all
   handled transparently by the `rest.Config` passed to `dynamic.NewForConfig`.

---

## Key Files Reference

| File | Purpose |
|---|---|
| `internal/config/types.go` | `GrafanaConfig`, `TLS`, `Context` data structures |
| `internal/config/rest.go` | `NewNamespacedRESTConfig()` — converts config to k8s REST config |
| `internal/config/stack_id.go` | `DiscoverStackID()` — auto-detect Grafana Cloud namespace |
| `internal/resources/dynamic/namespaced_client.go` | Primary CRUD client wrapping k8s dynamic.Interface |
| `internal/resources/dynamic/versioned_client.go` | Version-aware client for pull operations |
| `internal/resources/dynamic/errors.go` | `ParseStatusError()` / `APIError` — error translation |
| `internal/grafana/client.go` | OpenAPI client factory for /api operations |
| `internal/httputils/client.go` | Central HTTP client factory (`NewDefaultClient`, `NewClient`) |
| `internal/httputils/context.go` | `--log-http-payload` context threading |
| `internal/httputils/logger.go` | Debug-logging round-tripper |
| `internal/httputils/response.go` | HTTP response helpers for server handlers |
| `internal/resources/remote/remote.go` | `Processor` interface definition |
| `internal/resources/remote/puller.go` | `PullClient` interface, `Puller` using `VersionedClient` |
| `internal/resources/remote/pusher.go` | `PushClient` interface, `Pusher` using `NamespacedClient` |
| `internal/query/prometheus/client.go` | Prometheus query client using `rest.HTTPClientFor` |
| `internal/query/loki/client.go` | Loki query client using `rest.HTTPClientFor` |
