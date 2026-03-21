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
```

### Step 8: Smoke Test (MANDATORY — run after EACH phase)

Run every command side-by-side with gcx against a real instance. Don't skip
this — wrong endpoint names, wrapped request bodies, and response shape
mismatches are invisible in unit tests.

```bash
# For each command the provider exposes, compare gcx vs grafanactl:

# 1. List — check IDs match
gcx --context=dev {resource} list --limit 10
GRAFANACTL_AGENT_MODE=false grafanactl --context=dev {resource} list --limit 10
# Extract IDs from both, diff — must be identical

# 2. Get — check field values match
gcx --context=dev {resource} get {id} -o json
grafanactl --context=dev {resource} get {id} -o json
# Compare key fields (title, status, etc.) — must match exactly
# durationSeconds and similar computed fields may differ by seconds

# 3. Adapter path — verify resources pipeline works too
grafanactl --context=dev resources get {alias}
grafanactl --context=dev resources get {alias}/{id} -o json

# 4. Ancillary commands — verify each against gcx
# (severities, activity, roles, etc.)

# 5. Schema + example
grafanactl --context=dev resources schemas -o json | grep {group}
grafanactl --context=dev resources examples {alias}

# 6. Output formats — verify table, wide, json, yaml all render
GRAFANACTL_AGENT_MODE=false grafanactl --context=dev {resource} list -o table
GRAFANACTL_AGENT_MODE=false grafanactl --context=dev {resource} list -o wide
grafanactl --context=dev {resource} list -o json
grafanactl --context=dev {resource} list -o yaml
```

**Do NOT skip smoke tests.** The incidents port had two wrong endpoint names
that only surfaced during smoke testing:
- `SeverityService.GetSeverities` → actually `SeveritiesService.GetOrgSeverities`
- `ActivityService.QueryActivityItems` → actually `ActivityService.QueryActivity`

---

## Gotchas & Lessons Learned

> **Update this section** after each provider port.

### Auth

- **OnCall** uses a separate API URL discovered from the IRM plugin settings
  (`/api/plugins/grafana-irm-app/settings` → `jsonData.onCallApiUrl`). The same
  Grafana SA token is used, plus an `X-Grafana-Url` header with the stack URL.
  The config loader checks `GRAFANA_ONCALL_URL` env → provider config → auto-discovery.
  Three-tier fallback avoids mandatory config for most users.

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

### gRPC-style POST APIs (Incidents/IRM)

- The IRM API uses gRPC-style POST endpoints (`IncidentsService.QueryIncidents`,
  `IncidentsService.GetIncident`, etc.) — all operations are POST with JSON bodies,
  not REST-style GET/POST/PUT/DELETE. The `doRequest` helper always uses POST.
- gcx's `GetIncident` fetches all incidents (limit 100) and filters client-side.
  The actual API has a `GetIncident` endpoint — use it directly for O(1) lookups.
- The IRM API only supports status updates via `UpdateStatus` — there is no
  general-purpose PUT/PATCH for incident fields. The adapter's Update method
  extracts the status field and calls UpdateStatus.
- `FlexTime` is needed because the IRM API returns empty strings `""` for
  optional time fields instead of null. The `omitzero` tag (Go 1.24+) replaces
  `omitempty` for struct-typed fields to satisfy the modernize linter.
- Delete is not supported — the IRM API has no delete endpoint.
- Cursor-based pagination: the `contextPayload` field carries the cursor value
  between pages, not a separate cursor parameter.

### Multi-Resource Providers (OnCall pattern)

- For providers with many sub-resource types (OnCall has 12), use a generic
  `subResourceAdapter` with a `switch` dispatch on `kind` rather than 12 separate
  adapter files. This keeps the code in one package instead of 12 subpackages.
- Register all sub-resources under the same API group (`oncall.ext.grafana.app`)
  with different kinds (Integration, Schedule, AlertGroup, etc.).
- Use `oncall-*` prefixed aliases to avoid conflicts with core resource types
  (e.g., `oncall-teams` not `teams` to avoid clashing with K8s-native resources).
- The `X-Grafana-Url` header must use canonical Go form (`X-Grafana-Url` not
  `X-Grafana-URL`) or the `canonicalheader` linter will flag it. httptest servers
  receive the canonical form regardless of how you set it.

### Plugin Proxy APIs (Knowledge Graph / Asserts)

- KG/Asserts uses the Grafana plugin resource proxy path:
  `/api/plugins/grafana-asserts-app/resources/asserts/api-server/...`
- Auth: standard Grafana SA token via rest.Config — no separate token needed.
  gcx passes `X-Scope-OrgID: 0` but this is not required through the plugin proxy.
- The API is operational, not CRUD: many query endpoints (POST), config uploads
  (PUT with `application/x-yaml`), and read endpoints (GET).
- Rules are the closest to a standard resource (list/get/create/delete) and map
  well to the ResourceAdapter pipeline. Other sub-resources (datasets, entities,
  assertions) are best served as provider commands.
- The command tree is large (~20 subcommands) — use inline closures for each
  command rather than trying to share RunE builders.
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
| oncall | 12 sub-resources | ✅ done (2026-03-20) | Claude | All 12 sub-resources, iterator pagination, auto-discovery of OnCall URL |
| incidents | incidents | ✅ done (2026-03-20) | Claude | IRM plugin API, gRPC-style POST endpoints |
| k6 | projects, runs, envs | ⬜ planned | — | Multi-tenant auth, Phase 1.3 |
| fleet | pipelines, collectors, etc. | ⬜ planned | — | Phase 1.4 |
| kg | datasets, rules, entities, assertions, search | ✅ done (2026-03-20) | Claude | Plugin proxy API, 20+ subcommands, rules as ResourceAdapter |
| ml | jobs, holidays | ⬜ planned | — | Phase 1.6 |
| scim | users, groups | ⬜ planned | — | Phase 1.7 |
| gcom | access policies, stacks, etc. | ⬜ planned | — | Phase 1.8 |
| adaptive | metrics, logs, traces | ⬜ planned | — | Phase 1.9 |
| faro | apps | ⬜ planned | — | Phase 1.9 |
| grafana | annotations, lib panels, etc. | ⬜ planned | — | Phase 3 (non-K8s REST) |
| iam | permissions, RBAC, SSO, OAuth | ⬜ planned | — | Phase 3-4 |

---

## Tips for Complex Providers

> **Speculative** — written before these providers were ported. Validate
> and update during the actual port.

**OnCall** (12 sub-resources):
- Start with `integrations` — simplest, validates the pattern
- OnCall API URL discovered via GCOM, not configured directly
- Iterator-based pagination — port the pattern, don't simplify

**K6** (multi-tenant auth):
- Two auth modes: org-level and stack-level
- Separate API domain (not Grafana stack URL)
- Check gcx's `k6/client_envvar_test.go` for auth resolution logic

**Fleet/Alloy** (4 sub-resource types):
- All share same base URL and auth
- Single provider, four subpackages

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
