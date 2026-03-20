# gcx → grafanactl Provider Migration Recipe

> **Evergreen document.** Update this as providers are ported — add gotchas,
> refine patterns, fix mistakes. Each migration agent should read this before
> starting and update it after finishing.

## Overview

This recipe covers porting a gcx resource client (`pkg/grafana/{resource}/`)
into a grafanactl provider (`internal/providers/{name}/`). It's a streamlined
path that skips API discovery (gcx already has working clients) and focuses on
the mechanical translation.

**When to use this recipe:** Porting a gcx resource to grafanactl.
**When to use `/add-provider` instead:** Building a provider from scratch for a
product that doesn't have a gcx client.

---

## Pre-flight Checklist

Before starting a port, answer these questions:

```
[ ] 1. Is this resource already on K8s API?
      Run: grafanactl --context=ops resources schemas | grep -i {resource}
      If YES → no provider needed, it works via dynamic discovery.

[ ] 2. What's the gcx source?
      Client: pkg/grafana/{resource}/client.go
      Types:  pkg/grafana/{resource}/types.go (or inline in client.go)
      Cmd:    cmd/resources/{resource}.go (or cmd/observability/ or cmd/oncall/)

[ ] 3. Auth model?
      Same Grafana SA token: ConfigKeys = [] (reuse grafana.token)
      Separate token:        ConfigKeys = [{Name: "token", Secret: true}]
      Separate URL + token:  ConfigKeys = [{Name: "url"}, {Name: "token", Secret: true}]

[ ] 4. ID scheme?
      String UID:  metadata.name = uid (standard path)
      Integer ID:  metadata.name = strconv.Itoa(id) (needs int→string mapping)
      Composite:   metadata.name = slug-id or similar (document the scheme)

[ ] 5. Does it have cross-references?
      e.g., synth checks reference probes by ID. If yes, the adapter needs
      resolution logic in CreateFn/UpdateFn.

[ ] 6. Pagination?
      gcx uses manual pagination loops. Check if the API has limit/offset,
      cursor, or Link headers. The adapter's ListFn must handle this.
```

---

## Step-by-Step Port

### Step 1: Create provider package

```
internal/providers/{name}/
├── provider.go           # Provider interface + init() registration
├── {resource}/
│   ├── types.go          # API structs (copy from gcx, adjust json tags if needed)
│   ├── client.go         # HTTP client (adapt from gcx)
│   ├── adapter.go        # TypedRegistration[T] wiring
│   └── client_test.go    # httptest-based tests
```

**If adding to an existing provider** (e.g., adding a resource to `grafana` or
`iam`), skip creating `provider.go` — just add the resource subpackage and
register in the existing `init()`.

### Step 2: Port types.go

Copy structs from `gcx/pkg/grafana/{resource}/`. Adjustments:

- **Keep json tags exactly as gcx has them** — these match the API response
  format and must round-trip losslessly through pull → edit → push.
- **Remove gcx-specific helpers** (e.g., `func (t *Type) ResourceID() string`)
  — these are replaced by the adapter's `NameFn`.
- **Keep all fields** — don't prune "unnecessary" fields. The user may need them.

### Step 3: Port client.go

Translate from gcx's `grafana.Client` to grafanactl's HTTP pattern:

```go
// gcx pattern (before):
type Client struct {
    *grafana.Client  // embeds base client with .Get/.Post/.Put/.Delete
}

func NewClient(baseURL, token string) *Client {
    return &Client{grafana.NewClient(baseURL, token)}
}

func (c *Client) ListResources(ctx context.Context) ([]Resource, error) {
    var result []Resource
    err := c.Get(ctx, "/api/path", &result)
    return result, err
}
```

```go
// grafanactl pattern (after):
type Client struct {
    baseURL string
    token   string
    http    *http.Client
}

func NewClient(baseURL, token string) *Client {
    return &Client{
        baseURL: strings.TrimRight(baseURL, "/"),
        token:   token,
        http:    &http.Client{Timeout: 30 * time.Second},
    }
}

func (c *Client) List(ctx context.Context) ([]Resource, error) {
    req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/path", nil)
    if err != nil {
        return nil, err
    }
    req.Header.Set("Authorization", "Bearer "+c.token)
    resp, err := c.http.Do(req)
    // ... handle response, decode JSON
}
```

**Key differences:**
- No embedded base client — each provider owns its HTTP calls
- Explicit `context.Context` on all methods
- Direct `http.NewRequestWithContext` instead of gcx's `.Get()` wrapper
- Error handling: return `fmt.Errorf("{provider}: {action}: %w", err)` with
  provider name prefix for debuggability

**Pagination:** If gcx uses manual pagination loops, port them. If the API
returns all results in one call, keep it simple.

### Step 4: Wire adapter.go with TypedRegistration[T]

This is the part that `TypedResourceAdapter[T]` makes trivial:

```go
package {resource}

import (
    "context"
    "github.com/grafana/grafanactl/internal/resources/adapter"
)

func Register(loader ConfigLoader) {
    adapter.Register(adapter.TypedRegistration[ResourceType]{
        Descriptor: Descriptor(),
        Aliases:    []string{"{alias}"},
        GVK:        GVK(),
        Factory: func(ctx context.Context) (*adapter.TypedCRUD[ResourceType], error) {
            cfg, err := loader.Load(ctx)
            if err != nil {
                return nil, err
            }
            client := NewClient(cfg.BaseURL, cfg.Token)
            return &adapter.TypedCRUD[ResourceType]{
                Namespace: cfg.Namespace,
                NameFn:    func(r ResourceType) string { return r.UID },
                ListFn:    client.List,
                GetFn:     client.Get,
                CreateFn:  client.Create,
                UpdateFn:  client.Update,
                DeleteFn:  client.Delete,
            }, nil
        },
    })
}
```

**For int-ID resources**, the `NameFn` converts:
```go
NameFn: func(r Resource) string { return strconv.FormatInt(r.ID, 10) },
```

### Step 5: Register in init()

In `provider.go`:
```go
func init() {
    providers.Register(&Provider{})
    {resource}.Register(&configLoader{})
}
```

Add blank import in `cmd/grafanactl/root/command.go`:
```go
_ "github.com/grafana/grafanactl/internal/providers/{name}"
```

### Step 6: Write tests

Minimum test coverage per resource:

1. **Client tests** — httptest server returning known JSON, verify List/Get/Create/Update/Delete parse correctly
2. **Adapter round-trip** — create a typed object → adapter wraps it → unwrap back → compare (no data loss)

### Step 7: Verify

```bash
make all                                    # lint + tests + build + docs
grafanactl providers                        # new provider listed
grafanactl resources get {alias}            # returns data from real instance
grafanactl resources get {alias}/{id} -o json   # single resource
```

---

## Gotchas & Lessons Learned

> **Update this section** after each provider port.

### Auth

- **OnCall** uses a separate API URL and token, resolved via GCOM stack info.
  The adapter factory needs to call GCOM to discover the OnCall API URL before
  constructing the client.
  <!-- TODO: update after OnCall port -->

### ID Mapping

- **Integer IDs** (annotations, reports, teams): Store as `metadata.name =
  strconv.Itoa(id)`. The adapter's GetFn parses it back:
  `id, _ := strconv.ParseInt(name, 10, 64)`.
- **Slug+ID composites**: Some resources use `slug-123` patterns. Document the
  scheme in the adapter so future maintainers know how to decompose.

### Pagination

- gcx's `ListAll` pattern uses page+limit loops. Port these directly — don't
  try to be clever with streaming or lazy evaluation.
- Some APIs return wrapped responses (`{"items": [...], "totalCount": N}`).
  Define a `listResponse` struct per resource — don't try to share across types.

### Cross-References

- Synth checks reference probes by numeric ID. The adapter resolves probe
  names to IDs during Create/Update by calling the probe client. This logic
  lives in the adapter's `CreateFn`/`UpdateFn` closures.

### Response Shape Differences

- Some gcx clients unwrap response envelopes (e.g., `response.Data`) while
  others return the raw response. Check the gcx client carefully — the types
  you port must match what the API actually returns, not what gcx exposes.

---

## Provider Status Tracker

| Provider | Resources | Status | Ported By | Notes |
|----------|-----------|--------|-----------|-------|
| synth | checks, probes | ✅ existing | — | Reference impl, refactored to TypedAdapter in Phase 0 |
| slo | definitions, reports | ✅ existing | — | Reference impl |
| alert | rules, groups | ✅ existing | — | Read-only, expanding in Phase 2 |
| oncall | 12 sub-resources | ⬜ planned | — | Largest port, Phase 1.1 |
| incidents | incidents | ⬜ planned | — | IRM plugin API, Phase 1.2 |
| k6 | projects, runs, envs | ⬜ planned | — | Multi-tenant auth, Phase 1.3 |
| fleet | pipelines, collectors, etc. | ⬜ planned | — | Phase 1.4 |
| kg | datasets, rules, etc. | ⬜ planned | — | Phase 1.5 |
| ml | jobs, holidays | ⬜ planned | — | Phase 1.6 |
| scim | users, groups | ⬜ planned | — | Phase 1.7 |
| gcom | access policies, stacks, etc. | ⬜ planned | — | Phase 1.8 |
| adaptive | metrics, logs, traces | ⬜ planned | — | Phase 1.9 |
| faro | apps | ⬜ planned | — | Phase 1.9 |
| grafana | annotations, lib panels, etc. | ⬜ planned | — | Phase 3 (non-K8s REST) |
| iam | permissions, RBAC, SSO, OAuth | ⬜ planned | — | Phase 3-4 |

---

## Relationship to /add-provider Skill

This recipe is for **porting existing gcx clients**. The `/add-provider` skill
is for **building providers from scratch**. Key differences:

| Aspect | This Recipe | /add-provider Skill |
|--------|-------------|---------------------|
| API discovery | Skip — gcx has working client | Full discovery phase |
| Types | Copy from gcx | Derive from OpenAPI/source |
| Client | Adapt from gcx | Hand-write from scratch |
| Design doc | Optional (pattern is known) | Required per stage |
| Auth | Copy gcx's auth model | Investigate from scratch |

After porting, the provider should pass the same Phase 4 verification
checklist from `/add-provider`.
