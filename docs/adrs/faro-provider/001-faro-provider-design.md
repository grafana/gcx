# ADR-001: Faro Provider Design

**Created**: 2026-04-02
**Status**: proposed
**Issue**: https://github.com/grafana/gcx/issues/89

## Context

Faro (Frontend Observability) app management exists in the legacy gcx CLI but
has not been ported to gcx. Users cannot manage Faro apps through the new
tool. The gcx source lives in `cmd/observability/faro.go` (commands) and
`pkg/grafana/faro/faro.go` (client).

Faro is a cloud-only product. Its API is accessed via the Grafana plugin proxy
(`/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app`), using the
standard Grafana SA token. The API surface is small: 5 CRUD operations on
FaroApp, plus sourcemaps as a sub-resource of apps.

### Scope

**In scope (issue #89):** 5 CRUD commands on FaroApp (list, get, create,
update, delete) with full TypedCRUD adapter registration. 3 sourcemap
sub-resource commands (show-sourcemaps, apply-sourcemap, remove-sourcemap).

**Deferred:** `faro apps open` (browser link — no API calls, just URL
construction + browser launch).

## Decision

### 1. CLI UX (Stage 1A)

Command tree follows `$AREA $NOUN $VERB` grammar (CONSTITUTION § CLI Grammar).
Faro is a multi-resource provider (apps + sourcemaps), so apps get their own
noun level:

```
gcx faro
└── apps (app)
    ├── list   [--limit int]
    ├── get    [slug-id] [--name <name>]
    ├── create -f <file>
    ├── update [slug-id] -f <file> [--name <name>]
    ├── delete <slug-id>
    ├── show-sourcemaps <slug-id>
    ├── apply-sourcemap <slug-id> -f <file>
    └── remove-sourcemap <slug-id> <bundle-id>
```

**Sourcemaps are sub-resources** (CONSTITUTION § Sub-resources): every
sourcemap operation requires a parent app-id. They are NOT registered as
typed adapters and use alternative verbs (`show-`, `apply-`, `remove-`)
per CONSTITUTION § Provider-only resources must not mimic adapter verbs.

**Slug-ID convention**: Faro uses numeric IDs. Tables display composite
`metadata.name` (e.g. `my-faro-app-123`) as the `NAME` column. Bare
numeric IDs are accepted as input for backward compatibility.

### 2. Resource Adapters (Stage 1B)

| Resource | Adapter | Rationale |
|---|---|---|
| FaroApp | `TypedCRUD[FaroApp]` — full CRUD | All 5 operations supported |
| Sourcemaps | No adapter — sub-resource | Requires parent app-id, binary upload, composite key |

**GVK mapping:**

| Field | Value |
|---|---|
| Group | `faro.ext.grafana.app` |
| Version | `v1alpha1` |
| Kind | `FaroApp` |
| Singular | `faroapp` |
| Plural | `faroapps` |

**ResourceIdentity**: FaroApp implements `GetResourceName()` /
`SetResourceName()` using `adapter.SlugifyName` / `adapter.ExtractIDFromSlug`.

**Schema + Example**: Both non-nil on `adapter.Registration` (FaroApp is
writable).

### 3. Auth & Config (Stage 1C)

Plugin proxy API → standard Grafana SA token → no provider-specific config:

```go
func (p *FaroProvider) ConfigKeys() []providers.ConfigKey { return nil }
func (p *FaroProvider) Validate(cfg map[string]string) error { return nil }
```

Config resolution via `providers.ConfigLoader.LoadGrafanaConfig(ctx)`.
HTTP client via `rest.HTTPClientFor(&cfg.Config)` — correct for plugin
proxy calls that go through the Grafana server.

No `GRAFANA_PROVIDER_FARO_*` env vars needed.

### 4. Architecture (Stage 1D)

Flat package (single resource type, no subpackages):

```
internal/providers/faro/
├── provider.go              # Provider interface + init() registration
├── types.go                 # FaroApp, faroAppAPI, conversions
├── client.go                # HTTP client (explicit http.Client, not embedded grafana.Client)
├── client_test.go           # httptest-based tests
├── resource_adapter.go      # TypedCRUD factory, Descriptor, GVK, Schema, Example
├── resource_adapter_test.go # Adapter round-trip tests
├── commands.go              # CLI commands
├── commands_test.go         # Command tests
└── types_identity_test.go   # ResourceIdentity contract tests
```

**Client pattern**: Translated from gcx's embedded `grafana.Client` to
explicit `http.Client` + `host` fields with named endpoint methods. Internal
`toAPI()` / `fromAPI()` handles ExtraLogLabels (map ↔ array of `{key, value}`)
and ID (string ↔ int64).

**API quirks preserved from gcx source:**

| Quirk | Behavior | Source reference |
|---|---|---|
| ExtraLogLabels stripped on create | API returns 409 if included | `faro.go:171` |
| Settings stripped on create AND update | API returns 500 if included | `faro.go:173, 219` |
| Create re-fetches via List | Response missing collectEndpointURL/appKey | `faro.go:189` |
| Update requires ID in URL and body | API rejects otherwise | `faro.go:215-216` |
| GetByName is client-side filter | No server-side name lookup endpoint | `faro.go:148-164` |
| No retries on mutations | Uses PostNoRetry/PutNoRetry | `faro.go:175, 220` |

**Output compliance:**

| Command | Default format | Codecs | K8s wrapping |
|---|---|---|---|
| list | text | table + wide | Yes (json/yaml) |
| get | text | table (single-row) | Yes (json/yaml) |
| create | Status message | — | Created resource if `-o` |
| update | Status message | — | Updated resource if `-o` |
| delete | Status message | — | No |

Table columns: NAME (slug-id), AppKey, CollectEndpointURL.
Wide columns: + CORSOrigins count, ExtraLogLabels count, Settings summary.

## Consequences

- `gcx faro apps list/get/create/update/delete` provides ergonomic CRUD
- `gcx resources get faroapps` works via adapter for push/pull pipeline
- JSON/YAML output is identical between both paths (enforced by TypedCRUD)
- Sourcemaps deferred as sub-resource verbs under `apps` — clean extension point
- No provider-specific config simplifies onboarding
