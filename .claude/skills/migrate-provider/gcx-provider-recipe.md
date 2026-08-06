# Legacy grafana-cloud-cli → gcx Provider Migration Recipe

> **Evergreen document.** Update this as providers are ported — add gotchas,
> refine patterns, fix mistakes. Each migration agent should read this before
> starting and update it after finishing.

## Overview

This recipe covers porting a legacy `grafana-cloud-cli` resource client
(`pkg/grafana/{resource}/`) into a gcx provider
(`internal/providers/{name}/`). It starts from a working legacy client and
focuses on the mechanical translation after present-day placement is confirmed.

**When to use this recipe:** Porting a legacy CLI resource to gcx.
**When to use `/add-provider` instead:** Building a provider from scratch for a
product that doesn't have a legacy CLI client.

## Skill Structure

This recipe covers the **mechanical implementation steps only** (Steps 1-8), and
only its registration flow has been checked against current code — read SKILL.md's
opening caveat first. Workflow orchestration, phase gates, and verification are
governed by `SKILL.md`.

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
# 1. This repo's binary is built
bin/gcx --version

# 2. Grafana context is configured and working
bin/gcx config view
bin/gcx --context=<ctx> resources list-types | head -5

# 3. The legacy CLI is reachable under its OWN path and points at the same
#    Grafana. Substitute its real location; writing `gcx` here runs this repo's
#    binary again and compares it against itself.
"$LEGACY_CLI" --version

# 4. Provider directory structure exists
# Use /add-dir or create manually:
mkdir -p internal/providers/{name}/{resource}
```

If any of these fail, fix them before proceeding. Smoke tests (Phase 4) need live
access from **both** binaries — `bin/gcx` and `$LEGACY_CLI` — against the same
Grafana instance, and every comparison must keep the two visibly distinct. If no
instance is reachable, report each smoke test UNVERIFIED with the reason rather
than asserting parity.

---

## Pre-flight Checklist

Before starting a port, answer these questions:

```
[ ] 1. Is this resource externally accessible and discoverable on /apis?
      Run: bin/gcx --context=ops resources list-types | grep -i {resource}
      If YES → gcx resources already covers standard CRUD. Inventory the
      legacy non-CRUD operations separately and recheck whether dedicated
      provider commands are still needed.

[ ] 2. Where is the legacy CLI source?
      Client: pkg/grafana/{resource}/client.go
      Types:  pkg/grafana/{resource}/types.go (or inline in client.go)
      Cmd:    cmd/resources/{resource}.go (or cmd/observability/ or cmd/oncall/)

[ ] 3. Auth model?
      Same Grafana SA token: ConfigKeys = [] (reuse grafana.token)
      Separate token:        ConfigKeys = [{Name: "token", Secret: true}]
      Separate URL + token:  ConfigKeys = [{Name: "url"}, {Name: "token", Secret: true}]

[ ] 4. ID scheme?
      String UID:  metadata.name = uid (standard path)
      Integer ID:  prefer slug-id when a stable user-facing label exists;
                   accept bare numeric ID as input and as the no-label fallback
      Composite:   metadata.name = slug-id or similar (document the scheme)

[ ] 5. Does it have cross-references?
      e.g., synth checks reference probes by ID. If yes, the adapter needs
      resolution logic in CreateFn/UpdateFn.

[ ] 6. Pagination?
      If the legacy client uses manual pagination loops, port them. Check
      whether the API has limit/offset,
      cursor, or Link headers. The adapter's ListFn must handle this.
```

---

## Step-by-Step Port

### Step 1: Create provider package

```
internal/providers/{name}/
├── provider.go           # Provider interface + init() registration
├── {resource}/
│   ├── types.go          # API structs (copy from the legacy CLI; adjust tags if needed)
│   ├── client.go         # HTTP client (adapt from the legacy CLI)
│   ├── adapter.go        # TypedRegistration[T] wiring
│   └── client_test.go    # httptest-based tests
```

**If adding to an existing provider** (e.g., adding a resource to `grafana` or
`iam`), skip creating `provider.go`. Add the resource subpackage and return its
registration from the provider's existing `TypedRegistrations()` method. Do not
add another registration call to `init()`.

### Step 2: Port types.go

Copy structs from the legacy CLI's `pkg/grafana/{resource}/`. Adjustments:

- **Keep json tags exactly as the legacy CLI has them** — these match the API response
  format and must round-trip losslessly through pull → edit → push.
- **Replace legacy identity helpers** (e.g., `func (t *Type) ResourceID() string`)
  with `GetResourceName()` and `SetResourceName(string)` on the domain type, plus
  a compile-time `adapter.ResourceIdentity` assertion.
- **Keep all fields** — don't prune "unnecessary" fields. The user may need them.

### Step 3: Port client.go

Translate from the legacy CLI's `grafana.Client` to gcx's HTTP pattern:

```go
// Legacy CLI pattern (before):
type Client struct {
    *grafana.Client  // embeds base client with .Get/.Post/.Put/.Delete
}

func NewClient(baseURL, token string) *Client {
    return &Client{grafana.NewClient(baseURL, token)}
}

func (c *Client) ListResources(ctx context.Context) ([]ResourceType, error) {
    var result []ResourceType
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

func NewClient(ctx context.Context, baseURL, token string) *Client {
    return &Client{
        baseURL: strings.TrimRight(baseURL, "/"),
        token:   token,
        http:    httputils.NewDefaultClient(ctx),
    }
}

func (c *Client) List(ctx context.Context) ([]ResourceType, error) {
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
- `httputils.NewDefaultClient(ctx)` supplies gcx's standard timeout, logging,
  TLS defaults, and insecure-payload debug support
- Direct `http.NewRequestWithContext` instead of the legacy CLI's `.Get()` wrapper
- Error handling: return `fmt.Errorf("{provider}: {action}: %w", err)` with
  provider name prefix for debuggability

Follow `docs/reference/provider-guide.md` Step 4b for the destination-driven
client choice. `cloudCfg.HTTPClient(ctx)` is right for the Cloud host that
snapshot resolved; it does not inspect the URL you call, so a product API on its
own domain still needs `httputils.NewDefaultClient(ctx)`.

**Pagination:** If the legacy client uses manual pagination loops, port them. If
the API returns all results in one call, keep it simple.

### Step 4: Wire adapter.go with TypedRegistration[T]

This is the part that `TypedResourceAdapter[T]` makes trivial:

```go
package {resource}

import (
    "context"
    "github.com/grafana/gcx/internal/resources/adapter"
)

// ClientLoader is the resource-specific seam shared by commands and adapters.
// The function passed by the provider delegates config and auth resolution to
// providers.ConfigLoader.
type ClientLoader func(ctx context.Context) (*Client, string, error)

// Registration returns the typed registration for this resource. Collect these
// in the provider's TypedRegistrations() — the single providers.Register() call
// in the provider's init() performs the actual registration (never call
// adapter.Register() directly; CONSTITUTION § Architecture Invariants).
func Registration(loadClient ClientLoader) adapter.Registration {
    // ResourceType must implement adapter.ResourceIdentity
    // (GetResourceName/SetResourceName) — identity comes from the domain
    // type, not function pointers (CONSTITUTION § Architecture Invariants).
    return adapter.TypedRegistration[ResourceType]{
        Descriptor: Descriptor(),
        Aliases:    []string{"{alias}"},
        GVK:        GVK(),
        Schema:     resourceSchema(), // required, non-nil
        // Required for writable resources. CONSTITUTION § Architecture
        // Invariants permits a nil Example only when the resource has no
        // Create/Update support, because the example is the push template.
        Example: resourceExample(),
        Factory: func(ctx context.Context) (*adapter.TypedCRUD[ResourceType], error) {
            client, namespace, err := loadClient(ctx)
            if err != nil {
                return nil, err
            }
            return &adapter.TypedCRUD[ResourceType]{
                // Set Descriptor and Aliases on the CRUD too. ToRegistration()
                // copies them onto the Registration but NOT onto the CRUD, and
                // typedAdapter.Descriptor()/Aliases() read the CRUD's copies
                // (internal/resources/adapter/typed.go). Omit them and the
                // adapter's own Descriptor() is the zero value.
                Descriptor: Descriptor(),
                Aliases:    []string{"{alias}"},
                Namespace:  namespace,
                // LimitedListFn adapts a simple full-list client to TypedCRUD's
                // (ctx, limit) signature. If the backend accepts a limit, wire a
                // direct function that passes it through instead.
                ListFn: adapter.LimitedListFn(client.List),
                GetFn: func(ctx context.Context, name string) (*ResourceType, error) {
                    return client.Get(ctx, name)
                },
                CreateFn: func(ctx context.Context, item *ResourceType) (*ResourceType, error) {
                    return client.Create(ctx, item)
                },
                UpdateFn: func(ctx context.Context, name string, item *ResourceType) (*ResourceType, error) {
                    return client.Update(ctx, name, item)
                },
                DeleteFn: func(ctx context.Context, name string) error {
                    return client.Delete(ctx, name)
                },
            }, nil
        },
    }.ToRegistration()
}
```

The closures above show the signatures `TypedCRUD` requires. If a legacy
client uses numeric IDs, value parameters, or omits `name`, adapt that mismatch
inside the closure; assign a method directly only when its signature matches.

`ClientLoader` is a resource-local function type, not a method on
`providers.ConfigLoader`. Pass a provider-local method that delegates to the
real loader method for that auth model (`LoadGrafanaConfig`, `LoadCloudConfig`,
`LoadProviderConfig`, or `LoadDirectProviderSnapshot`) and constructs the client
according to `docs/reference/provider-guide.md` Steps 4 and 4b. There is no
generic `ConfigLoader.Load`. See `internal/providers/irm/config.go` and
`oncall_adapter.go` for the production shape.

> **On `ToRegistration()`:** it is a convenience wrapper with no production
> callers today — every shipped provider builds `adapter.Registration{...}`
> directly, setting `Descriptor` on both the Registration and the CRUD. See
> `internal/providers/irm/oncall_adapter.go` (`crud := &adapter.TypedCRUD[T]{…
> Descriptor: desc}`) for the shape the repo actually uses. Either form is
> compliant; the direct form is the one with precedent.

**For numeric-ID resources with a stable user-facing label**, implement
`ResourceIdentity` with the slug-id helpers in
`internal/resources/adapter/slug.go` (`SlugifyName`, `ComposeName`,
`ExtractIDFromSlug`) so `metadata.name` stays human-readable. Accept a bare
numeric ID as input and use it as the fallback when no stable label exists.

### Step 5: Register in init()

`init()` contains exactly one call — `providers.Register()` — and nothing else.
That single call populates both the provider registry and the adapter registry,
by invoking `adapter.Register()` for every entry your `TypedRegistrations()`
returns. A second registration call in `init()` violates
CONSTITUTION.md § Architecture Invariants ("Unified provider registration").

In `provider.go`:
```go
func init() {
    providers.Register(&Provider{})
}

// configLoader embeds the canonical loader. Implement one distinctly named
// client-loading method per resource using the matching providers.ConfigLoader
// method described above.
type configLoader struct {
    providers.ConfigLoader
}

// TypedRegistrations collects the adapter registrations for this provider's
// resources. Construct the provider-local client loader here and thread it into
// each resource's Registration() — the loader is not registered separately.
func (p *Provider) TypedRegistrations() []adapter.Registration {
    loader := &configLoader{}
    return []adapter.Registration{
        {resource}.Registration(loader.Load{Resource}Client),
        // ...one entry per adapter-backed resource
    }
}
```

Commands-only providers return `nil` from `TypedRegistrations()` — that is
first-class, not a stub. Reference implementation:
`internal/providers/irm/provider.go` (`init()` plus `TypedRegistrations()`
assembling registrations from two resource families through one loader).

> **Note:** The blank import in `cmd/gcx/root/command.go` is added in Step 7
> (Integration / Wiring), not here. Step 5 only covers `provider.go`.

### Step 6: Write tests

Minimum test coverage per resource:

1. **Client tests** — httptest server returning known JSON, verify List/Get/Create/Update/Delete parse correctly
2. **Adapter round-trip** — create a typed object → adapter wraps it → unwrap back → compare (no data loss)

### Step 7: Integration / Wiring

After Build-Core and Build-Commands are complete, the integration task MUST:

1. **Implement `Commands()` and `TypedRegistrations()`** as methods on the
   provider — `init()` itself stays a single `providers.Register()` call (Step 5)
2. **Add blank import** in `cmd/gcx/root/command.go` — the root command mounts
   every *registered* provider's commands automatically, but nothing registers
   until the package is linked in, and only the blank import does that
3. **Fix import cycles** introduced by subpackage references
4. **Fix variable name collisions** from package aliasing
5. **Run `mise run lint`** and fix all new issues

```bash
GCX_AGENT_MODE=false mise run all    # MUST exit 0 — this is the Phase 3 gate
bin/gcx providers list                   # new provider listed
```

### Step 8: Smoke Test (Phase 4 — MANDATORY)

> **Phase 4 verification.** This step maps to Phase 4 steps 4A–4E in SKILL.md.
> Smoke tests are MANDATORY to attempt and MUST NOT be silently skipped. If no
> live instance is available, report each one UNVERIFIED with that reason and do
> not assert parity with the legacy CLI.
>
> Every show/list command MUST be tested with ALL FOUR output formats:
> `-o json`, `-o table`, `-o wide`, `-o yaml`.

Run every command side-by-side — the **legacy** CLI at `$LEGACY_CLI` against
**this repo's** `bin/gcx` — on a real instance. Don't skip this: wrong endpoint
names, wrapped request bodies and response-shape mismatches are invisible in unit
tests.

Every pair below must invoke *two different binaries*. If both sides of a
comparison read the same, the diff will report MATCH while proving nothing —
that is the failure mode this template is written to make obvious.

#### 8a. Structured Comparison (jq diff template)

```bash
CTX=dev                      # adjust to your context
LEGACY_CLI=/path/to/legacy   # the OLD CLI, under its own path — never `gcx`
NEW_CLI=bin/gcx              # this repo's freshly built binary

# Guard: refuse to run a comparison against this CLI on both sides. Comparing
# the two path strings is not enough — since the rename, LEGACY_CLI can name
# THIS CLI three different ways, each invisible to the check before it.
for c in "$LEGACY_CLI" "$NEW_CLI"; do
  [ -x "$c" ] || { echo "FATAL: $c is not an executable file — build bin/gcx and set LEGACY_CLI to the legacy binary's own path"; exit 1; }
done
# 1. Same file reached by two paths (a symlink, or /usr/local/bin/gcx -> here).
[ ! "$LEGACY_CLI" -ef "$NEW_CLI" ] || {
  echo "FATAL: LEGACY_CLI and NEW_CLI are the same file"; exit 1; }
# 2. A copy of the same build under another path.
cmp -s "$LEGACY_CLI" "$NEW_CLI" && {
  echo "FATAL: LEGACY_CLI and NEW_CLI are byte-identical copies"; exit 1; }
# 3. A DIFFERENT gcx build — an older install that inherited the name. Different
#    inode and different bytes, so checks 1 and 2 both pass it. This is the
#    likeliest form of the collision. The probe below is PARTIAL: it catches gcx
#    builds new enough to expose 'agent skills', not ones predating it.
"$LEGACY_CLI" agent skills list >/dev/null 2>&1 && {
  echo "FATAL: $LEGACY_CLI answers 'agent skills list', so it is a gcx build, not the legacy CLI"; exit 1; }
# No automated check proves the remainder, and "does it call itself gcx?" cannot
# settle it — the legacy binary was named `gcx` too, so its own help says `gcx`.
# Before trusting any MATCH below, confirm by hand on positive evidence: run
# `$LEGACY_CLI --help` and check the subcommands are the legacy tool's surface
# (the one you are porting from), and that gcx-only trees are absent — it should
# have no `resources` tier, which is why the adapter checks below run new-side
# only.

# Both sides must succeed before any diff means anything. Two failed commands
# produce two empty outputs, and `diff` reports those as identical — a MATCH
# that proves nothing. Check status explicitly; pipefail keeps a failing CLI
# from being masked by a successful `jq`.
set -o pipefail

# --- List: compare resource IDs ---
"$LEGACY_CLI" --context=$CTX {resource} list -o json > /tmp/legacy_list.json \
  || { echo "List: LEGACY COMMAND FAILED — no comparison possible"; exit 1; }
"$NEW_CLI" --context=$CTX {resource} list -o json > /tmp/new_list.json \
  || { echo "List: NEW COMMAND FAILED — no comparison possible"; exit 1; }
LEGACY_IDS=$(jq -r '.[].id // .[].uid' /tmp/legacy_list.json | sort)
NEW_IDS=$(jq -r '.[].metadata.name' /tmp/new_list.json | sort)
[ -n "$LEGACY_IDS" ] || { echo "List: legacy returned zero ids — pick a context with data"; exit 1; }
echo "=== List ID diff ==="
diff <(echo "$LEGACY_IDS") <(echo "$NEW_IDS") && echo "MATCH" || echo "MISMATCH"

# --- Get: compare key fields ---
ID="<pick-an-id-from-list>"
"$LEGACY_CLI" --context=$CTX {resource} get $ID -o json \
  | jq '{title, status, labels}' > /tmp/legacy_get.json \
  || { echo "Get: LEGACY COMMAND FAILED — no comparison possible"; exit 1; }
"$NEW_CLI" --context=$CTX {resource} get $ID -o json \
  | jq '{title: .spec.title, status: .spec.status, labels: .metadata.labels}' > /tmp/new_get.json \
  || { echo "Get: NEW COMMAND FAILED — no comparison possible"; exit 1; }
[ -s /tmp/legacy_get.json ] || { echo "Get: legacy produced no output"; exit 1; }
echo "=== Get field diff ==="
diff /tmp/legacy_get.json /tmp/new_get.json && echo "MATCH" || echo "MISMATCH"

# --- Adapter path (new side only — the legacy CLI has no resources tier) ---
echo "=== Adapter path ==="
"$NEW_CLI" --context=$CTX resources get {alias} > /dev/null 2>&1 && echo "resources get: OK" || echo "resources get: FAIL"
"$NEW_CLI" --context=$CTX resources get {alias}/$ID -o json > /dev/null 2>&1 && echo "resources get/id: OK" || echo "resources get/id: FAIL"

# --- Ancillary commands (repeat per ancillary) ---
echo "=== Ancillary: {subcommand} ==="
"$LEGACY_CLI" --context=$CTX {resource} {subcommand} -o json | jq length
"$NEW_CLI"    --context=$CTX {resource} {subcommand} -o json | jq length

# --- Schema + example ---
echo "=== Schema ==="
"$NEW_CLI" --context=$CTX resources list-types -o json | jq 'to_entries[] | select(.key | test("{group}")) | .value' | head -5
echo "=== Example ==="
"$NEW_CLI" --context=$CTX resources list-examples {alias} | head -10

# --- Output format check ---
echo "=== Output formats ==="
for fmt in table wide json yaml; do
  GCX_AGENT_MODE=false "$NEW_CLI" --context=$CTX {resource} list -o $fmt > /dev/null 2>&1 \
    && echo "$fmt: OK" || echo "$fmt: FAIL"
done
```

#### 8b. Paste Results

Copy the output from 8a into the conversation. For each comparison:

| Check | Expected | Action if fails |
|-------|----------|-----------------|
| List ID diff | `MATCH` | Fix `ListFn` or the type's `GetResourceName()` implementation |
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

- **OnCall** reaches the IRM app on the Grafana stack host through the plugin
  resource path — `BasePath = "/api/plugins/grafana-irm-app/resources"`
  (`internal/providers/irm/oncall_client.go`) — using the same Grafana SA token.
  Because the destination is `cfg.Host`, this is not a direct-auth provider and
  needs no separate URL or token in `ConfigKeys`. Earlier revisions of this
  recipe described a separate GCOM-discovered `onCallApiUrl`; check the client
  before porting anything that assumes it.

### ID Mapping

- **Integer IDs** (annotations, reports, teams): prefer a slug-id name when the
  API exposes a stable user-facing label. Accept a bare numeric ID as input and
  use it as `metadata.name` only when no stable label exists. Restore the API ID
  with `ExtractIDFromSlug` or `ExtractInt64IDFromSlug`.
- **Slug+ID composites**: Some resources use `slug-123` patterns. Document the
  scheme in the adapter so future maintainers know how to decompose.

### Pagination

- The legacy CLI's `ListAll` pattern uses page+limit loops. Port these directly
  — don't try to be clever with streaming or lazy evaluation.
- Some APIs return wrapped responses (`{"items": [...], "totalCount": N}`).
  Define a `listResponse` struct per resource — don't try to share across types.

### Cross-References

- Synth checks reference probes by numeric ID. The adapter resolves probe
  names to IDs during Create/Update by calling the probe client. This logic
  lives in the adapter's `CreateFn`/`UpdateFn` closures.

### gRPC-style POST APIs (Incidents/IRM)

- The IRM API uses gRPC-style POST endpoints (`IncidentsService.QueryIncidentPreviews`,
  `IncidentsService.GetIncident`, etc.) under the versioned `/resources/api/v1/`
  base path — all operations are POST with JSON bodies, not REST-style
  GET/POST/PUT/DELETE. The `doRequest` helper always uses POST.
  (`QueryIncidents` is deprecated upstream; gcx queries previews and filters
  via the query-string language, e.g. `label:"security"`. Undocumented
  services like `SeveritiesService` and `IncidentContextService` 404 under
  `/v1` and stay on the unversioned base path.)
- The legacy CLI's `GetIncident` fetches all incidents (limit 100) and filters
  client-side. The actual API has a `GetIncident` endpoint — use it directly for
  O(1) lookups.
- The IRM API only supports status updates via `UpdateStatus` — there is no
  general-purpose PUT/PATCH for incident fields. The adapter's Update method
  extracts the status field and calls UpdateStatus.
- `FlexTime` is needed because the IRM API returns empty strings `""` for
  optional time fields instead of null. The `omitzero` tag (Go 1.24+) replaces
  `omitempty` for struct-typed fields to satisfy the modernize linter.
- Delete is not supported — the IRM API has no delete endpoint.
- Cursor-based pagination: the cursor is a top-level request field next to
  `query` (`{"query": {...}, "cursor": {"nextValue": "..."}}`) — pass the
  previously returned cursor back verbatim to fetch the next page. It is not
  a field inside the query object. Page `limit` is capped at 100 by the API.

### Token Exchange Auth (k6)

- k6 uses a **separate API domain** (`api.k6.io`), not the Grafana stack URL.
- Auth requires a two-step token exchange: AP token → k6 v3 token via
  `PUT /v3/account/grafana-app/start` with `X-Grafana-Key`, `X-Stack-Id`,
  `X-Grafana-Service-Token` headers.
- The stack ID is not simply "parse the namespace". k6 resolves credentials and
  destination through `LoadDirectProviderSnapshot` and caches the resolved stack
  id (`cached-stack-id` in `internal/providers/k6/cache.go`); the namespace is
  parsed and validated only on the proxy path
  (`internal/providers/k6/resource_adapter.go`). Read that resolution before
  assuming a `stack-{id}` namespace is the source of truth.
- The org ID (needed for env vars) comes from the auth response, not config.
- The `perfsprint` linter enforces `errors.New` over `fmt.Errorf` for strings
  without format verbs — easy to miss when porting `fmt.Errorf("...")` patterns.
- The `usestdlibvars` linter enforces `http.StatusCreated` etc. instead of
  raw `201`/`204`/`404` literals — the legacy CLI uses raw numbers in many
  places.
- **Legacy `$LEGACY_CLI k6 token` vs current `bin/gcx k6 auth token`**: the
  legacy CLI exposes token exchange as a top-level `token` subcommand; gcx
  nests it under `auth token`.
  Both print the short-lived API token to stdout.
- **Schedules `delete` takes `<load-test-id>` not `<schedule-id>`**: This
  is consistent with the API — delete is keyed on the load test, not the
  schedule object. Preserve that behavior from the legacy CLI.
- **`runs` appears in two places**: `k6 runs list` (top-level) and
  `k6 testrun runs list` (nested under testrun). Both delegate to the same
  underlying run listing function. The duplication is intentional — the
  `testrun` sub-tree groups CRD-related operations together.
- **Legacy `schema` / `example` subcommands**: the legacy CLI exposes these
  under each resource group. gcx covers them through
  `resources list-types` and `resources list-examples` at the global level.
  These are NOT missing — the coverage is different but equivalent.

### Multi-Resource Providers (OnCall pattern)

- For providers with many sub-resource types (OnCall has 12), use a generic
  `subResourceAdapter` with a `switch` dispatch on `kind` rather than 12 separate
  adapter files. This keeps the code in one package instead of 12 subpackages.
- Register all sub-resources under the same API group (`oncall.ext.grafana.app`)
  with different kinds (Integration, Schedule, AlertGroup, etc.).
- Use `oncall-*` prefixed aliases to avoid conflicts with core resource types
  (e.g., `oncall-teams` not `teams` to avoid clashing with K8s-native resources).
- Any custom header must be written in canonical Go form (`X-Grafana-Url`, not
  `X-Grafana-URL`) or the `canonicalheader` linter will flag it. httptest servers
  receive the canonical form regardless of how you set it. (The IRM client no
  longer sends `X-Grafana-Url` at all — the rule is what generalizes, not that
  header.)

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
### Plugin Proxy APIs (Faro / Frontend Observability)

- Faro uses two different plugin proxy base paths:
  - CRUD: `/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app`
  - Sourcemaps: `/api/plugins/grafana-kowalski-app/resources/api/v1/app/{id}/sourcemaps`
- Auth: standard Grafana SA token via `rest.HTTPClientFor` — no separate token needed.
- **API quirks preserved from the legacy CLI source:**
  - Create MUST strip `ExtraLogLabels` (API returns 409) and `Settings` (API returns 500).
  - Update MUST strip `Settings` (API returns 500).
  - Create response is incomplete (missing `collectEndpointURL`, `appKey`) — must re-fetch
    via List after creation to get full details.
  - Update requires ID in both URL path and request body.
  - `GetByName` is client-side: list all apps, filter by name (no server-side endpoint).
- **Wire format conversion:** `ExtraLogLabels` is `map[string]string` in Go but
  `[]{"key": k, "value": v}` on the wire. `ID` is `string` in Go but `int64` on wire.
  Internal `toAPI()`/`fromAPI()` handles both conversions.
- **Sourcemaps are sub-resources** (require parent app-id for all operations).
  Per CONSTITUTION § Sub-resources, they use `<operation>-<subject>` compounds
  addressed by the parent's ID (`list-sourcemaps`, `apply-sourcemap`,
  `delete-sourcemap`) and are NOT adapter-registered.
- **Sourcemaps plugin endpoint returns 500** on dev/ops instances as of 2026-04-02.
  This is a Faro plugin bug, not a gcx code issue. The request is correctly
  constructed (verified via `-vvv` debug logging).
- **Resource plural is `apps`** (not `faroapps`), so the full GVK selector is
  `apps.v1alpha1.faro.ext.grafana.app`. Short form: `resources get apps`.

### Response Shape Differences

- Some legacy clients unwrap response envelopes (e.g., `response.Data`) while
  others return the raw response. Check the legacy client carefully — the types
  you port must match what the API actually returns, not what it exposes.

### Separate API URLs (Fleet, OnCall)

- Fleet Management uses a separate API URL, not the Grafana instance URL, but it
  does **not** take that URL from provider config: `FleetProvider.ConfigKeys()`
  returns nil and the destination comes from discovered stack metadata via
  `ConfigLoader.LoadCloudConfig` (`internal/providers/fleet/provider.go`,
  `internal/fleet/config.go`). There is no `LoadFleetConfig`; synth's
  `LoadSMConfig` is a provider-local method, not a pattern to name-match against.
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
| kg | rules, scopes, entities, assertions, search, insights | ✅ done (2026-03-20) | Claude | Plugin proxy API; typed resources: rules + scopes |
| ml | jobs, holidays | ⬜ planned | — | Phase 1.6 |
| scim | users, groups | ⬜ planned | — | Phase 1.7 |
| gcom | access policies, stacks, etc. | ⬜ planned | — | Phase 1.8 |
| adaptive | metrics, logs, traces | ⬜ planned | — | Phase 1.9 |
| faro | apps, sourcemaps | ✅ done (2026-04-02) | Claude | Plugin proxy API, TypedCRUD[FaroApp], sourcemaps as sub-resource verbs. Sourcemap smoke blocked by Faro plugin 500. |
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

**k6** (multi-tenant auth):
- Two auth modes: org-level and stack-level
- Separate API domain (not Grafana stack URL)
- Check the legacy CLI's `k6/client_envvar_test.go` for auth resolution logic

**Fleet/Alloy** (4 sub-resource types):
- All share same base URL and auth
- Single provider, four subpackages

---

## Relationship to /add-provider Skill

This recipe is for **porting existing legacy CLI clients**. The `/add-provider`
skill is for **building providers from scratch**. Key differences:

| Aspect | This Recipe | /add-provider Skill |
|--------|-------------|---------------------|
| API discovery | Skip — the legacy CLI has a working client | Full discovery phase |
| Types | Copy from the legacy CLI | Derive from OpenAPI/source |
| Client | Adapt from the legacy CLI | Hand-write from scratch |
| Design doc | Optional (pattern is known) | Required per stage |
| Auth | Preserve the legacy protocol behavior (headers, token exchange, basic vs bearer), but resolve credentials and endpoints through current `ConfigLoader` rules | Investigate from scratch |

After porting, the provider must pass Phase 4 verification (SKILL.md steps
4A–4E) including mandatory smoke tests with all four output formats.
