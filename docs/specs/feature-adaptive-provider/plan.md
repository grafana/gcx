---
type: feature-plan
title: "Adaptive Telemetry Provider"
status: done
spec: docs/specs/feature-adaptive-provider/spec.md
created: 2026-03-27
---

# Architecture and Design Decisions

## Pipeline Architecture

```
                           ┌──────────────────────────────┐
                           │  gcx adaptive                │
                           │  (single Cobra parent)       │
                           └──────┬───────────────────────┘
                                  │
              ┌───────────────────┼───────────────────┐
              ▼                   ▼                   ▼
     ┌────────────────┐  ┌────────────────┐  ┌────────────────┐
     │ metrics        │  │ logs           │  │ traces         │
     │ (commands.go)  │  │ (commands.go)  │  │ (commands.go)  │
     └───────┬────────┘  └───────┬────────┘  └───────┬────────┘
             │                   │                   │
             │  show only        │  show + CRUD      │  show + CRUD
             ▼                   ▼                   ▼
     ┌────────────────┐  ┌────────────────┐  ┌────────────────┐
     │ metrics/       │  │ logs/          │  │ traces/        │
     │ client.go      │  │ client.go      │  │ client.go      │
     │ (HTTP)         │  │ (HTTP)         │  │ (HTTP)         │
     └───────┬────────┘  └───────┬────────┘  └───────┬────────┘
             │                   │                   │
             └─────────┬─────────┴─────────┬─────────┘
                       ▼                   ▼
              ┌────────────────┐  ┌────────────────────────┐
              │ auth.go        │  │ providers.ConfigLoader  │
              │ (shared Basic  │  │ LoadCloudConfig()       │
              │  auth helper)  │  │ SaveProviderConfig()    │
              └───────┬────────┘  └────────────────────────┘
                      │
                      ▼
              ┌────────────────┐
              │ cloud.StackInfo│
              │ (GCOM)        │
              └────────────────┘

Resource Pipeline Integration (adapter tier):

  gcx get/push/pull/delete exemptions   ─┐
  gcx get/push/pull/delete policies     ─┤
                                         ▼
                              ┌──────────────────┐
                              │ adapter.Registry  │
                              │ (TypedCRUD[T])   │
                              └──────┬───────────┘
                                     │
                     ┌───────────────┴───────────────┐
                     ▼                               ▼
            ┌────────────────┐              ┌────────────────┐
            │ logs/          │              │ traces/        │
            │ resource_      │              │ resource_      │
            │ adapter.go     │              │ adapter.go     │
            │ TypedCRUD      │              │ TypedCRUD      │
            │ [Exemption]    │              │ [Policy]       │
            └────────────────┘              └────────────────┘
```

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Single `auth.go` at `internal/providers/adaptive/` with helper functions for all three signals | All three signals use identical Basic auth (`{instanceID}:{apiToken}`) against signal-specific GCOM fields. A shared helper avoids triple-duplicating GCOM lookup and caching logic. Implements FR-004 through FR-006. |
| Auth helper returns `(baseURL, instanceID, apiToken, *http.Client, error)` tuple | Each signal client needs these four values to construct its HTTP client. Returning a tuple avoids introducing a new struct while keeping the call site explicit. |
| GCOM caching via `SaveProviderConfig()` at keys `cloud.{signal}-tenant-id` / `cloud.{signal}-tenant-url` | Reuses the existing `ConfigLoader.SaveProviderConfig()` and `LoadProviderConfig()` pattern from fleet. Config key prefix `cloud.` groups all cloud-derived cached values. Implements FR-005a. |
| Each signal package owns its own `client.go` (not a shared generic client) | The three APIs have different endpoint structures (REST vs gRPC-style), response shapes, and HTTP methods. Forcing a shared generic client would create complexity without reducing code. |
| `show` verb for provider-only read-only collections, NOT `list` | CONSTITUTION.md verb mimicry prohibition: `list` is reserved for adapter-backed resources that participate in `gcx get`. Provider-only collections use `show` to avoid semantic confusion. |
| TypedRegistration[T] for Exemption and Policy adapters | Uses the established `TypedRegistration[T].ToRegistration()` pattern (see SLO definitions). Schema via `SchemaFromType[T]()`, example via static JSON builder. Implements FR-019 through FR-021. |
| Metrics subpackage has NO adapter registration | Metrics resources (rules, recommendations) do not support individual CRUD — only bulk sync and list. They remain provider-only `show`/`sync` commands. Implements spec decision on adapter eligibility. |
| Options pattern for all commands: `opts struct` + `setup(flags)` + `Validate()` | Matches every other provider in the codebase (fleet, slo, synth). Each command's opts struct embeds `cmdio.Options` and registers table/wide codecs. |
| Dry-run flag on all destructive commands via `--dry-run` bool flag on opts | Flag checked before the HTTP call; when set, the command prints what would happen and returns without mutation. Implements FR-018. |

## HTTP Client Reference

### Auth helper (`auth.go`)

```go
// ResolveSignalAuth checks cached config first, falls back to GCOM.
// signal: "metrics" | "logs" | "traces"
// Returns: baseURL, tenantID (int), apiToken (string), httpClient, error
func ResolveSignalAuth(ctx context.Context, loader *providers.ConfigLoader, signal string) (
    string, int, string, *http.Client, error)
```

The helper:
1. Calls `loader.LoadProviderConfig(ctx, "adaptive")` to check for cached `{signal}-tenant-id` / `{signal}-tenant-url`
2. If cache hit → use cached values, skip GCOM
3. If cache miss → call `loader.LoadCloudConfig(ctx)` → GCOM lookup → extract signal-specific fields from `StackInfo`
4. Write resolved values to config via `loader.SaveProviderConfig(ctx, "adaptive", "{signal}-tenant-id", id)` (and URL)
5. Return `(baseURL, tenantID, cloudCfg.Token, providers.ExternalHTTPClient(), nil)`

Each signal client uses the returned tuple to set Basic auth per-request:

```go
req.SetBasicAuth(strconv.Itoa(tenantID), apiToken)
```

### Per-signal endpoints

**Metrics** (base: `StackInfo.HMInstancePromURL`)

| Method | Path | Purpose | Notes |
|--------|------|---------|-------|
| GET | `/aggregations/rules` | List rules | Returns `[]MetricRule`, ETag in response header |
| POST | `/aggregations/rules` | Replace all rules | Requires `If-Match: {etag}` header |
| GET | `/aggregations/recommendations` | List recommendations | Returns `[]MetricRecommendation` |

**Logs** (base: `StackInfo.HLInstanceURL`)

| Method | Path | Purpose | Notes |
|--------|------|---------|-------|
| GET | `/adaptive-logs/exemptions` | List exemptions | Response wrapped in `{"result": [...]}` |
| POST | `/adaptive-logs/exemptions` | Create exemption | Returns created `Exemption` |
| PUT | `/adaptive-logs/exemptions/{id}` | Update exemption | ID is URL-escaped |
| DELETE | `/adaptive-logs/exemptions/{id}` | Delete exemption | ID is URL-escaped |
| GET | `/adaptive-logs/recommendations` | List patterns | Returns `[]LogRecommendation` |
| POST | `/adaptive-logs/recommendations` | Apply patterns | Full array replacement |

**Traces** (base: `StackInfo.HTInstanceURL`)

| Method | Path | Purpose | Notes |
|--------|------|---------|-------|
| GET | `/adaptive-traces/api/v1/policies` | List policies | Returns `[]Policy` |
| GET | `/adaptive-traces/api/v1/policies/{id}` | Get policy | ID is URL-escaped |
| POST | `/adaptive-traces/api/v1/policies` | Create policy | Returns created `Policy` |
| PUT | `/adaptive-traces/api/v1/policies/{id}` | Update policy | ID is URL-escaped |
| DELETE | `/adaptive-traces/api/v1/policies/{id}` | Delete policy | ID is URL-escaped |
| GET | `/adaptive-traces/api/v1/recommendations` | List recommendations | Returns `[]Recommendation` |
| POST | `/adaptive-traces/api/v1/recommendations/{id}/apply` | Apply recommendation | No body |
| POST | `/adaptive-traces/api/v1/recommendations/{id}/dismiss` | Dismiss recommendation | No body |

### Client construction pattern

Each signal's `client.go` follows this structure (example: metrics):

```go
type Client struct {
    baseURL    string
    tenantID   int
    apiToken   string
    httpClient *http.Client
}

func NewClient(baseURL string, tenantID int, apiToken string, httpClient *http.Client) *Client {
    return &Client{
        baseURL:    strings.TrimRight(baseURL, "/"),
        tenantID:   tenantID,
        apiToken:   apiToken,
        httpClient: httpClient,
    }
}

func (c *Client) doRequest(ctx context.Context, method, path string, body any) (*http.Response, error) {
    var reqBody io.Reader
    if body != nil {
        data, _ := json.Marshal(body)
        reqBody = bytes.NewReader(data)
    }
    req, _ := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
    req.Header.Set("Content-Type", "application/json")
    req.SetBasicAuth(strconv.Itoa(c.tenantID), c.apiToken)
    return c.httpClient.Do(req)
}
```

## Compatibility

**Unchanged:**
- All existing providers continue to register and function identically
- All existing `gcx get`, `push`, `pull`, `delete` commands work as before
- The adapter registry adds two new GVKs but does not modify any existing registrations
- Config file structure is unchanged; new cached keys are additive under `cloud.*`

**Newly available:**
- `gcx adaptive metrics rules show` — list aggregation rules
- `gcx adaptive metrics rules sync` — sync aggregation rules from file or recommendations
- `gcx adaptive metrics recommendations show` — list metric recommendations
- `gcx adaptive metrics recommendations apply` — apply metric recommendations as rules
- `gcx adaptive logs patterns show` — list log patterns/recommendations
- `gcx adaptive logs patterns apply` — apply log pattern drop rates
- `gcx adaptive traces recommendations show` — list trace recommendations
- `gcx adaptive traces recommendations apply` — apply trace recommendations
- `gcx adaptive traces recommendations dismiss` — dismiss trace recommendations
- `gcx resources get exemptions.v1alpha1.adaptive-logs.ext.grafana.app` — via adapter
- `gcx resources get policies.v1alpha1.adaptive-traces.ext.grafana.app` — via adapter
- `gcx resources push/pull/delete` for exemptions and policies — via adapter
- `gcx resources schemas` for Exemption and Policy — JSON Schema output
