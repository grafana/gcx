---
type: feature-plan
title: "App O11y Provider"
status: draft
spec: docs/specs/feature-appo11y-provider/spec.md
created: 2026-03-30
---

# Architecture and Design Decisions

## Pipeline Architecture

```
gcx appo11y {overrides,settings} {get,update}
    │
    ├── get ──► ConfigLoader.LoadGrafanaConfig()
    │              ├── rest.HTTPClientFor() ──► *http.Client
    │              └── appo11y.Client
    │                    ├── GetOverrides()  ──► GET  /api/plugin-proxy/grafana-app-observability-app/overrides
    │                    └── GetSettings()   ──► GET  /api/plugin-proxy/grafana-app-observability-app/provisioned-plugin-settings
    │              ──► TypedCRUD[T].GetFn ──► TypedObject[T] ──► cmdio.Encode (table/wide/json/yaml)
    │
    └── update -f ──► format.Detect(file) ──► TypedObject[T]
                       └── TypedCRUD[T].Update("default", typedObj)
                             └── UpdateFn
                                   ├── extract ETag from annotation (overrides only)
                                   └── appo11y.Client
                                         ├── UpdateOverrides(spec, etag) ──► POST /overrides (If-Match: etag)
                                         └── UpdateSettings(spec)         ──► POST /provisioned-plugin-settings
                       ──► status code check only ──► cmdio.Success

gcx resources get overrides.v1alpha1.appo11y.ext.grafana.app/default
    │
    └── adapter.Registry ──► appo11y TypedCRUD[T].AsAdapter() ──► same GetFn path
```

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Shared HTTP client at `internal/providers/appo11y/client.go` | Both resource kinds hit the same Grafana plugin proxy host with the same auth. A single `Client` struct with four methods (GetOverrides, UpdateOverrides, GetSettings, UpdateSettings) avoids duplication. Follows SLO pattern where `definitions/client.go` holds all HTTP methods. (FR-008, FR-009, FR-010, FR-011, FR-012) |
| `MetadataFn` on Overrides TypedCRUD to inject ETag annotation | The `TypedCRUD.MetadataFn` callback merges extra fields into envelope metadata. Returning `{"annotations": {"appo11y.ext.grafana.app/etag": etag}}` injects the ETag into the K8s envelope without modifying the TypedCRUD framework. The ETag is carried as an unexported field on the Overrides domain type. (FR-005, FR-006) |
| Overrides domain type carries unexported `etag string` field | Unexported fields are ignored by `json.Marshal`, so the ETag does not leak into the `spec`. The client populates it from the HTTP response header; the `MetadataFn` reads it for the annotation; the `UpdateFn` extracts it from the incoming resource's annotation to send `If-Match`. (FR-005, FR-006, NC-002) |
| Per-kind subpackages `overrides/` and `settings/` | Matches the SLO provider structure (`definitions/`, `reports/`). Each subpackage owns its types, adapter wiring, and CLI commands. The provider root (`provider.go`) wires them together. (FR-026) |
| Singleton name hardcoded to `"default"` | Both resources are singletons. `GetResourceName()` always returns `"default"`. The `GetFn` ignores the `name` parameter (or validates it equals `"default"`). (FR-003) |
| `ListFn`, `CreateFn`, `DeleteFn` all nil | The API has no collection or lifecycle endpoints. Setting these to nil causes `TypedCRUD` to return `errors.ErrUnsupported` automatically. (FR-007, NC-003) |
| Table codecs registered per-kind via `cmdio.Options.RegisterCustomCodec` | Each subpackage defines its own `tableCodec` with `Wide` variant. Overrides shows 5 columns (7 wide), Settings shows 3 columns (5 wide). (FR-013 through FR-018) |
| Update commands go through TypedCRUD | Per CONSTITUTION.md, provider CRUD commands must use TypedCRUD typed methods, not raw API clients. The `-f` flag accepts both YAML and JSON. The file is decoded into a `TypedObject[T]`, then passed to `TypedCRUD.Update()`. The `UpdateFn` closure extracts the ETag annotation (overrides) and calls the client. This ensures both provider commands and `resources push` use identical logic. (FR-004) |
| Error translation for 404 and 412 | Client maps HTTP 404 to "Grafana App Observability plugin is not installed or not enabled" and HTTP 412 to "concurrent modification conflict — re-fetch and retry". (NC-001, Risk mitigation) |
| Blank import in `cmd/gcx/root/command.go` | Adding `_ "github.com/grafana/gcx/internal/providers/appo11y"` triggers `init()` self-registration. Same wiring as all other providers. (FR-001) |

## Compatibility

**Continues working unchanged:**
- All existing providers and their CLI commands
- `gcx resources get`, `gcx resources schemas`, `gcx resources examples` — they gain new entries but existing behavior is unaffected
- `gcx resources push -f` and `gcx resources pull .../default` work through the generic adapter pipeline (confirmed in spec Open Questions)

**Newly available:**
- `gcx appo11y overrides get [-o table|wide|json|yaml]`
- `gcx appo11y overrides update -f <file>`
- `gcx appo11y settings get [-o table|wide|json|yaml]`
- `gcx appo11y settings update -f <file>`
- Generic paths: `gcx resources get overrides.v1alpha1.appo11y.ext.grafana.app/default`
- Generic paths: `gcx resources get settings.v1alpha1.appo11y.ext.grafana.app/default`

## HTTP Client Reference

| Operation | Method | Endpoint | Request Body | Response Body | Headers |
|-----------|--------|----------|-------------|---------------|---------|
| Get Overrides | GET | `/api/plugin-proxy/grafana-app-observability-app/overrides` | — | `MetricsGeneratorConfig` JSON | Response: `ETag` |
| Update Overrides | POST | `/api/plugin-proxy/grafana-app-observability-app/overrides` | `MetricsGeneratorConfig` JSON | Discarded (status check only) | Request: `If-Match: <etag>` (when annotation present), `Content-Type: application/json` |
| Get Settings | GET | `/api/plugin-proxy/grafana-app-observability-app/provisioned-plugin-settings` | — | `PluginSettings` JSON | — |
| Update Settings | POST | `/api/plugin-proxy/grafana-app-observability-app/provisioned-plugin-settings` | `PluginSettings` JSON | Discarded (status check only) | Request: `Content-Type: application/json` |

**Auth**: Standard Grafana SA token via `rest.HTTPClientFor()` — the `rest.Config` from `ConfigLoader` injects `Authorization: Bearer <token>` automatically.

**Client construction**:
```go
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
    httpClient, err := rest.HTTPClientFor(&cfg.Config)
    // ...
    return &Client{host: cfg.Host, httpClient: httpClient}, nil
}
```

**Error mapping** (plain errors, matching SLO/fleet convention):
- HTTP 404 → `"Grafana App Observability plugin is not installed or not enabled"`
- HTTP 412 → `"concurrent modification conflict: overrides were modified since last read — re-fetch and retry"`
- Other non-2xx → generic `"request failed with status %d: %s"` (body parsed for error message, raw body never exposed)

**Note**: Errors are plain `fmt.Errorf`, not `k8s.io/apimachinery/pkg/api/errors.StatusError`.
This matches all existing providers (SLO, fleet, oncall, etc.). For singleton resources
this is correct — the pusher's `apierrors.IsNotFound()` check is never reached because
singletons always exist (Get succeeds → Update path). A cross-cutting issue to standardize
all provider error handling to use `apierrors` types is tracked separately.
