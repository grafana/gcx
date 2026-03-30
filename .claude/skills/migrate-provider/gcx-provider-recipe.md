# gcx → gcx Provider Migration Recipe

> **Evergreen document.** Update this as providers are ported — add gotchas,
> refine patterns, fix mistakes. Each migration agent should read this before
> starting and update it after finishing.

## Overview

This recipe covers porting a gcx resource client (`pkg/grafana/{resource}/`)
into a gcx provider (`internal/providers/{name}/`). It's a streamlined
path that skips API discovery (gcx already has working clients) and focuses on
the mechanical translation.

**When to use this recipe:** Porting a gcx resource to gcx.
**When to use `/add-provider` instead:** Building a provider from scratch for a
product that doesn't have a gcx client.

## Skill Structure

This recipe covers the **mechanical implementation steps only** (Steps 1-8).
Workflow orchestration, phase gates, and verification are governed by
`SKILL.md` — read it before starting any migration.

- **Orchestration** is defined in SKILL.md's five-phase pipeline (Phase 0–4).
- **Phase gates** in SKILL.md control when you may proceed between phases.
- **Phase 0** (Requirements Gathering) produces the context bundle autonomously.
- **Phase 1** (Design Discovery) produces the ADR via interactive brainstorming.
- **Phase 2** (Spec Planning) produces spec.md + plan.md + tasks.md.
- **Phase 3** (Build) executes this recipe's mechanical steps (Steps 1-8).
- **Phase 4** (Verification) runs smoke tests and produces the comparison report.

If you are an agent reading this recipe: your orchestration comes from SKILL.md.
This recipe provides the mechanical steps only.

## Spec Document Format

Phase 2 produces three documents that replace the old custom audit artifacts
(parity table, architectural mapping, verification plan):

- **spec.md** — functional requirements with FR-NNN numbering + acceptance
  criteria in Given/When/Then format. Replaces the parity table.
- **plan.md** — architecture decisions + HTTP client reference section (endpoint
  table, auth signature, client construction). Replaces the architectural mapping.
- **tasks.md** — dependency graph with waves + per-task deliverables including
  mandatory smoke test tasks for all four output formats. Replaces the
  verification plan.

All three documents use YAML frontmatter. See `commands-reference.md` for the
HTTP client reference section template that plan.md must include.

---

## Prerequisites

Verify these before starting any port:

```bash
# 1. gcx binary is available
gcx --version

# 2. Grafana context is configured and working
gcx config view
gcx --context=<ctx> resources schemas | head -5

# 3. gcx uses the same context (or configure separately)
gcx --context=<ctx> health

# 4. Provider directory structure exists
# Use /add-dir or create manually:
mkdir -p internal/providers/{name}/{resource}
```

If any of these fail, fix them before proceeding. Smoke tests (Phase 4) require
live API access to both gcx and gcx against the same Grafana instance.

---

## Pre-flight Checklist

Before starting a port, answer these questions:

```
[ ] 1. Is this resource already on K8s API?
      Run: gcx --context=ops resources schemas | grep -i {resource}
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

Translate from gcx's `grafana.Client` to gcx's HTTP pattern:

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
// gcx pattern (after):
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
    "github.com/grafana/gcx/internal/resources/adapter"
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

> **Note:** The blank import in `cmd/gcx/root/command.go` is added in Step 7
> (Integration / Wiring), not here. Step 5 only covers `provider.go`.

### Step 6: Write tests

Minimum test coverage per resource:

1. **Client tests** — httptest server returning known JSON, verify List/Get/Create/Update/Delete parse correctly
2. **Adapter round-trip** — create a typed object → adapter wraps it → unwrap back → compare (no data loss)

### Step 7: Integration / Wiring

After Build-Core and Build-Commands are complete, the integration task MUST:

1. **Wire `Commands()` and `TypedRegistrations()`** in the provider's `init()`
2. **Add blank import** in `cmd/gcx/root/command.go`
3. **Fix import cycles** introduced by subpackage references
4. **Fix variable name collisions** from package aliasing
5. **Run `make lint`** and fix all new issues

```bash
GCX_AGENT_MODE=false make all    # MUST exit 0 — this is the Phase 3 gate
gcx providers                    # new provider listed
```

### Step 8: Smoke Test (Phase 4 — MANDATORY)

> **Phase 4 verification.** This step maps to Phase 4 steps 4A–4E in SKILL.md.
> Smoke tests are MANDATORY — they MUST NOT be marked "optional" or "if live
> instance available". If no live instance is available, block and report.
>
> Every show/list command MUST be tested with ALL FOUR output formats:
> `-o json`, `-o table`, `-o wide`, `-o yaml`.

Run every command side-by-side with gcx against a real instance. Don't skip
this — wrong endpoint names, wrapped request bodies, and response shape
mismatches are invisible in unit tests.

#### 8a. Structured Comparison (jq diff template)

```bash
CTX=dev  # adjust to your context

# --- List: compare resource IDs ---
GCX_IDS=$(gcx --context=$CTX {resource} list -o json | jq -r '.[].id // .[].uid' | sort)
GCTL_IDS=$(gcx --context=$CTX {resource} list -o json | jq -r '.[].metadata.name' | sort)
echo "=== List ID diff ==="
diff <(echo "$GCX_IDS") <(echo "$GCTL_IDS") && echo "MATCH" || echo "MISMATCH"

# --- Get: compare key fields ---
ID="<pick-an-id-from-list>"
gcx --context=$CTX {resource} get $ID -o json | jq '{title, status, labels}' > /tmp/gcx_get.json
gcx --context=$CTX {resource} get $ID -o json \
  | jq '{title: .spec.title, status: .spec.status, labels: .metadata.labels}' > /tmp/gctl_get.json
echo "=== Get field diff ==="
diff /tmp/gcx_get.json /tmp/gctl_get.json && echo "MATCH" || echo "MISMATCH"

# --- Adapter path ---
echo "=== Adapter path ==="
gcx --context=$CTX resources get {alias} > /dev/null 2>&1 && echo "resources get: OK" || echo "resources get: FAIL"
gcx --context=$CTX resources get {alias}/$ID -o json > /dev/null 2>&1 && echo "resources get/id: OK" || echo "resources get/id: FAIL"

# --- Ancillary commands (repeat per ancillary) ---
echo "=== Ancillary: {subcommand} ==="
gcx --context=$CTX {resource} {subcommand} -o json | jq length
gcx --context=$CTX {resource} {subcommand} -o json | jq length

# --- Schema + example ---
echo "=== Schema ==="
gcx --context=$CTX resources schemas -o json | jq 'to_entries[] | select(.key | test("{group}")) | .value' | head -5
echo "=== Example ==="
gcx --context=$CTX resources examples {alias} | head -10

# --- Output format check ---
echo "=== Output formats ==="
for fmt in table wide json yaml; do
  GCX_AGENT_MODE=false gcx --context=$CTX {resource} list -o $fmt > /dev/null 2>&1 \
    && echo "$fmt: OK" || echo "$fmt: FAIL"
done
```

#### 8b. Paste Results

Copy the output from 8a into the conversation. For each comparison:

| Check | Expected | Action if fails |
|-------|----------|-----------------|
| List ID diff | `MATCH` | Fix ListFn or adapter NameFn |
| Get field diff | `MATCH` (computed fields like `durationSeconds` may differ by seconds) | Fix types or ToResource mapping |
| Adapter path | `OK` | Fix resource_adapter registration |
| Ancillary counts | Equal | Fix endpoint name or response parsing |
| Schema/example | Non-empty | Fix register.go |
| Output formats | All `OK` | Fix codec registration |

> **STOP.** Do not pass the Phase 4 gate until all checks pass or discrepancies
> are explicitly justified (e.g., "durationSeconds differs by 2s — acceptable").

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

### Token Exchange Auth (K6)

- K6 uses a **separate API domain** (`api.k6.io`), not the Grafana stack URL.
- Auth requires a two-step token exchange: AP token → k6 v3 token via
  `PUT /v3/account/grafana-app/start` with `X-Grafana-Key`, `X-Stack-Id`,
  `X-Grafana-Service-Token` headers.
- The stack ID can be parsed from the gcx namespace (`stack-{id}`),
  avoiding the need for a separate GCOM call.
- The org ID (needed for env vars) comes from the auth response, not config.
- The `perfsprint` linter enforces `errors.New` over `fmt.Errorf` for strings
  without format verbs — easy to miss when porting `fmt.Errorf("...")` patterns.
- The `usestdlibvars` linter enforces `http.StatusCreated` etc. instead of
  raw `201`/`204`/`404` literals — gcx uses raw numbers everywhere.
- **gcx `k6 token` vs gcx `k6 auth token`**: gcx exposes token exchange
  as a top-level `token` subcommand; gcx nests it under `auth token`.
  Both print the short-lived API token to stdout.
- **Schedules `delete` takes `<load-test-id>` not `<schedule-id>`**: This
  is consistent with the API — delete is keyed on the load test, not the
  schedule object. This is also how gcx does it.
- **`runs` appears in two places**: `k6 runs list` (top-level) and
  `k6 testrun runs list` (nested under testrun). Both delegate to the same
  underlying run listing function. The duplication is intentional — the
  `testrun` sub-tree groups CRD-related operations together.
- **gcx `schema` / `example` subcommands**: gcx exposes per-resource `schema`
  and `example` subcommands under each resource group. gcx covers these
  via `resources schemas` and `resources examples` at the global level.
  These are NOT missing — the coverage is different but equivalent.

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

### Separate API URLs (Fleet, OnCall)

- Fleet Management uses a separate API URL, not the Grafana instance URL.
  Use `ConfigKeys` with `url`, `instance-id`, `token` for provider config.
  The configLoader pattern from synth (`LoadFleetConfig` vs synth's `LoadSMConfig`)
  works well — extract credentials from `providers["fleet"]` config map + env vars.
- Fleet uses basic auth (`instance-id:token`) when instance-id is set,
  otherwise Bearer token. The `NewClient(url, instanceID, token, useBasicAuth)` pattern
  handles both modes via the `useBasicAuth` flag.
- Discovery and instrumentation commands need additional context (prom cluster/instance IDs)
  that currently require GCOM stack info — not ported yet, deferred to GCOM provider.

---

## Provider Status Tracker

| Provider | Resources | Status | Ported By | Notes |
|----------|-----------|--------|-----------|-------|
| synth | checks, probes | ✅ existing | — | Reference impl, refactored to TypedAdapter in Phase 0 |
| slo | definitions, reports | ✅ existing | — | Reference impl |
| alert | rules, groups | ✅ existing | — | Read-only, expanding in Phase 2 |
| oncall | 12 sub-resources | ✅ done (2026-03-20) | Claude | All 12 sub-resources, iterator pagination, auto-discovery of OnCall URL |
| incidents | incidents | ✅ done (2026-03-20) | Claude | IRM plugin API, gRPC-style POST endpoints |
| k6 | projects, tests, runs, envs, schedules, load-zones, envvars | ✅ done + verified (2026-03-24) | Claude | Token exchange auth, separate API domain. Full command tree verified live against dev context. Schedules, load-zones, and testrun CRD commands added beyond original scope. |
| fleet | pipelines, collectors, tenant | ✅ done (2026-03-20) | Claude | gRPC/Connect API, separate URL + basic auth, 3 resource types |
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

After porting, the provider must pass Phase 4 verification (SKILL.md steps
4A–4E) including mandatory smoke tests with all four output formats.
