---
type: feature-plan
title: "Faro Provider Architecture"
status: draft
spec: docs/specs/faro-provider/spec.md
created: 2026-04-02
---

# Architecture and Design Decisions

## Pipeline Architecture

```
CLI Layer (cmd/gcx/root/command.go)
    └── blank import: _ "github.com/grafana/gcx/internal/providers/faro"
         │
         ▼
Provider Registration (internal/providers/faro/provider.go)
    └── init() → providers.Register(&FaroProvider{})
         │
         ├── Commands() → []*cobra.Command
         │    └── "faro" parent
         │         └── "apps" subcommand group
         │              ├── list / get / create / update / delete  (CRUD)
         │              ├── show-sourcemaps / apply-sourcemap / remove-sourcemap  (sub-resource)
         │              └── all use providers.ConfigLoader for auth
         │
         └── TypedRegistrations() → []adapter.Registration
              └── FaroApp TypedCRUD adapter
                   ├── ListFn → client.List
                   ├── GetFn → client.Get (extracts numeric ID from slug)
                   ├── CreateFn → client.Create (strips labels+settings, re-fetches)
                   ├── UpdateFn → client.Update (ID in URL+body, strips settings)
                   └── DeleteFn → client.Delete

Data Flow (CRUD):
    User input → ConfigLoader → rest.HTTPClientFor → Client
        → GET/POST/PUT/DELETE to cfg.Host + basePath
            → Grafana plugin proxy → Faro backend

Data Flow (Sourcemaps):
    User input → ConfigLoader → rest.HTTPClientFor → Client
        → GET/POST/DELETE to cfg.Host + sourcemapPath(appID)
            → Grafana plugin resource proxy → Faro backend
```

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Flat package (no subpackages) | Single CRUD resource type (FaroApp); sourcemaps are sub-resource verbs, not a separate adapter |
| `rest.HTTPClientFor` for HTTP client | Plugin proxy goes through Grafana server — k8s transport bearer token injection is correct |
| Explicit `http.Client` + `host` (not embedded `grafana.Client`) | Recipe Step 3: translate to typed HTTP client with named endpoint methods |
| `toAPI()` / `fromAPI()` internal conversion | ExtraLogLabels (map↔array) and ID (string↔int64) conversion stays internal to client |
| Create strips ExtraLogLabels + Settings | Faro API bugs: 409 on labels, 500 on settings — preserved from gcx source |
| Create re-fetches via List | Create response missing collectEndpointURL/appKey — same pattern as gcx |
| No retries on mutations | PostNoRetry/PutNoRetry pattern from gcx — mutations are not idempotent |
| Sourcemaps use separate base path | `/api/plugins/grafana-kowalski-app/resources/api/v1/app/{id}/sourcemaps` — different plugin route |
| Sourcemaps use alternative verbs | CONSTITUTION § Provider-only resources must not mimic adapter verbs |

## HTTP Client Reference

### Endpoint Table — FaroApp CRUD

| Method | Path | Purpose | Notes |
|--------|------|---------|-------|
| GET | `/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app` | List all apps | Returns `[]faroAppAPI` at top level (no wrapper) |
| GET | `/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/{id}` | Get single app | Returns single `faroAppAPI` object |
| POST | `/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app` | Create app | Strip ExtraLogLabels + Settings; 409 on name conflict |
| PUT | `/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/{id}` | Update app | ID in both URL and body; strip Settings |
| DELETE | `/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/{id}` | Delete app | Returns 204 No Content |

### Endpoint Table — Sourcemaps

| Method | Path | Purpose | Notes |
|--------|------|---------|-------|
| GET | `/api/plugins/grafana-kowalski-app/resources/api/v1/app/{id}/sourcemaps` | List sourcemap bundles | Different base path than CRUD |
| POST | `/api/plugins/grafana-kowalski-app/resources/api/v1/app/{id}/sourcemaps` | Upload sourcemap bundle | JSON body, not multipart |
| DELETE | `/api/plugins/grafana-kowalski-app/resources/api/v1/app/{id}/sourcemaps/{bundleId}` | Delete sourcemap bundle | Returns 204 |

### Auth Pattern

```go
// Auth is handled by rest.HTTPClientFor — the k8s transport round-tripper
// injects the Bearer token from cfg.Config.BearerToken on every request.
// No custom auth headers needed.
//
// Faro uses the standard Grafana SA token through the plugin proxy.
// No extra headers (no X-Grafana-Url, no X-Scope-OrgID).
```

### Client Construction Pattern

```go
type Client struct {
    httpClient *http.Client  // from rest.HTTPClientFor(&cfg.Config)
    host       string        // from cfg.Host (Grafana server URL)
}

func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
    httpClient, err := rest.HTTPClientFor(&cfg.Config)
    if err != nil {
        return nil, fmt.Errorf("faro: failed to create HTTP client: %w", err)
    }
    return &Client{httpClient: httpClient, host: cfg.Host}, nil
}
```

### Wire Format Types (internal, not exported)

```go
// faroAppAPI is the wire format — ID is int64, ExtraLogLabels is array
type faroAppAPI struct {
    ID                 int64            `json:"id,omitempty"`
    Name               string           `json:"name"`
    AppKey             string           `json:"appKey,omitempty"`
    CollectEndpointURL string           `json:"collectEndpointURL,omitempty"`
    CORSOrigins        []CORSOrigin     `json:"corsOrigins,omitempty"`
    ExtraLogLabels     []LogLabel       `json:"extraLogLabels,omitempty"`
    Settings           *FaroAppSettings `json:"settings,omitempty"`
}

// Deserialization — copy verbatim from gcx source:
// List: json.Unmarshal(body, &[]faroAppAPI{})  — top-level array, no wrapper
// Get:  json.Unmarshal(body, &faroAppAPI{})    — single object, no wrapper
```

## Compatibility

- **Continues working unchanged:** All existing providers, `resources` pipeline, config system
- **Newly available:** `gcx faro apps` commands, `gcx resources get faroapps` adapter path
- **No deprecation:** This is a new provider, nothing is replaced
