# Synthetic Monitoring Provider Implementation Plan

## Context

grafanactl needs a Synthetic Monitoring (SM) provider as a Wave 2 provider
(`grafanactl-experiments-a4l`). SM is a Grafana Cloud product that runs HTTP,
ping, TCP, DNS, gRPC, and scripted checks from distributed probe nodes.

Unlike the SLO provider (which reuses the Grafana SA token), SM uses a
**separate service URL and token** — the SM API is hosted at a dedicated endpoint
and authenticated with an SM-specific access token.

Source bead: `grafanactl-experiments-a4l`

## Key Design Decisions

### 1. Auth: Separate `sm_url` + `sm_token`

SM runs as a standalone service, not a Grafana plugin. It has its own base URL
and its own access token.

```go
func (p *SynthProvider) ConfigKeys() []providers.ConfigKey {
    return []providers.ConfigKey{
        {Name: "sm_url", Description: "Synthetic Monitoring API URL (e.g. https://synthetic-monitoring-api.grafana.net)"},
        {Name: "sm_token", Secret: true, Description: "Synthetic Monitoring access token"},
    }
}
```

Env var overrides: `GRAFANA_SM_URL`, `GRAFANA_SM_TOKEN`.

### 2. API Client: Plain HTTP with Bearer token

SM exposes a REST API at `{sm_url}/api/v1/`. Not a K8s-compatible API.
Hand-roll the HTTP client with `Authorization: Bearer <sm_token>`.

**Verified endpoints** (tested against live API):

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/api/v1/check/list` | List all checks |
| `GET` | `/api/v1/check/{id}` | Get single check |
| `POST` | `/api/v1/check/add` | Create check |
| `POST` | `/api/v1/check/update` | Update check (body must include id+tenantId) |
| `DELETE` | `/api/v1/check/delete/{id}` | Delete check |
| `GET` | `/api/v1/probe/list` | List all probes |
| `GET` | `/api/v1/tenant` | Get tenant info (tenantId source) |

### 3. K8s Envelope

```
apiVersion: syntheticmonitoring.ext.grafana.app/v1alpha1
kind: Check
```

### 4. Prepare/Unprepare Pattern for ID Round-Tripping

SM checks have server-assigned integer IDs. Update calls require both `id` and
`tenantId` in the request body.

**Pull (Unprepare → K8s)**:
- `metadata.name` = strconv.FormatInt(check.ID, 10)  — numeric string
- Strip `id`, `tenantId`, `created`, `modified` from spec
- Resolve probe IDs → probe names in `spec.probes`

**Push (Prepare → API)**:
- If `metadata.name` parses as int64 → **update**: inject `id` + `tenantId` into body
- If `metadata.name` does not parse as int64 → **create**: POST to `/check/add` without `id`
- After create: update local file `metadata.name` to the server-assigned numeric ID
- `tenantId` always fetched from `GET /api/v1/tenant` at push time (cached once per push)

### 5. Probe Name ↔ ID Resolution

The API stores probes as integer IDs. Local YAML stores probe **names** (human-readable).

- On **pull**: probe IDs in check response → resolved to names via probe list cache
- On **push**: probe names in YAML → resolved to IDs via probe list cache before POST/PUT

If a probe name cannot be resolved, return a clear error: `probe "TypoName" not found; run grafanactl synth probes list`.

### 6. Package Layout

```
internal/providers/synth/
├── provider.go              # Provider interface impl + init() + configLoader
├── provider_test.go         # Interface contract tests
├── checks/
│   ├── types.go             # Check, Label, CheckSettings, probe types
│   ├── client.go            # HTTP client: list/get/create/update/delete
│   ├── client_test.go       # httptest unit tests
│   ├── adapter.go           # K8s envelope ↔ API translation + name⇔ID + probe resolution
│   ├── adapter_test.go      # Round-trip property tests
│   └── commands.go          # list/get/push/pull subcommands
└── probes/
    ├── types.go             # Probe, ProbeCapabilities
    ├── client.go            # HTTP client: list (read-only)
    ├── client_test.go
    ├── adapter.go           # K8s envelope adapter for probes
    ├── adapter_test.go
    └── commands.go          # probes list subcommand
```

Note: Probe management (add/update/delete) is out of scope — probes are
infrastructure managed by Grafana. We expose read-only probe discovery.

## Command Surface

```
grafanactl synth                            Manage Grafana Synthetic Monitoring resources
├── checks                                  Manage SM checks
│   ├── list                                List all checks
│   ├── get <id>                            Get a specific check by ID
│   ├── push [path...]                      Create or update checks from local files
│   └── pull                                Pull all checks to local files
└── probes                                  Manage SM probes
    └── list                                List available probes by region
```

**Stage 2 additions** (not in this plan):
```
└── checks
    ├── status [id]                         Show check success rate (Prometheus: probe_success)
    └── timeline [id] [--window 6h]         Show pass/fail over time (range query + graph)
```

## Implementation Stages

Two stages:

- [Stage 1: Checks CRUD + Probes List](1-checks-probes-crud/2026-03-06-checks-probes-crud.md) (~1,200 LOC)
- [Stage 2: Checks Status + Timeline](2-checks-status/2026-03-06-checks-status.md) (~400 LOC, deferred)

Stage 2 requires a Grafana Prometheus datasource configured in the grafanactl
context for `probe_success` metric queries (same pattern as SLO status).

## Dependency Graph

```
Stage 1: Checks CRUD + Probes List
    │
    └──► Stage 2: Checks Status + Timeline (needs check client + Prom query pattern)
```

## File Tree (Stage 1)

```
internal/providers/synth/
├── provider.go
├── provider_test.go
├── checks/
│   ├── types.go
│   ├── client.go
│   ├── client_test.go
│   ├── adapter.go
│   ├── adapter_test.go
│   └── commands.go
└── probes/
    ├── types.go
    ├── client.go
    ├── client_test.go
    ├── adapter.go
    ├── adapter_test.go
    └── commands.go

cmd/grafanactl/root/command.go   # add blank import
```

## Table Output Designs

### `synth checks list`

```
ID      JOB                                  TARGET                                  TYPE    ENABLED  PROBES
6247    Mimir: mimir-dev-10 GET root         https://prometheus-dev-10.grafana.net   http    true     Oregon, Spain
6248    atlantis /healthz: dev-us-east-0     https://atlantis-webhooks.grafana.net   http    true     Oregon
6249    k6 browser smoke test                https://app.example.com                 browser true     Paris, Oregon
```

### `synth probes list`

```
ID    NAME     REGION  ONLINE  DEPRECATED  VERSION
70    Paris    EMEA    false   false       v0.8.2
166   Oregon   AMER    true    false       v0.11.11
217   Spain    EMEA    true    false       v0.11.11
```

## API Route Map (Stage 1 scope)

| # | Method | Path | Purpose | Verified |
|---|--------|------|---------|---------|
| 1 | GET | `/api/v1/check/list` | List all checks | ✓ |
| 2 | GET | `/api/v1/check/{id}` | Get single check | ✓ |
| 3 | POST | `/api/v1/check/add` | Create check | ✓ |
| 4 | POST | `/api/v1/check/update` | Update check | — |
| 5 | DELETE | `/api/v1/check/delete/{id}` | Delete check | ✓ |
| 6 | GET | `/api/v1/probe/list` | List probes | ✓ |
| 7 | GET | `/api/v1/tenant` | Get tenant info | ✓ |

## Areas of Uncertainty

- **Update path**: The Go client uses `POST /check/update` (not `PUT /check/{id}`).
  The OpenAPI spec shows both patterns. Confirmed via Go client source that
  `/check/update` is the correct path.
- **Probe resolution on partial failure**: If some probe names are invalid during
  push, fail the entire check or report errors per-check? Plan: fail fast with
  error listing all unresolved probe names.
- **Tenant ID caching**: `GET /api/v1/tenant` adds one request per push session.
  Cache the result for the duration of the command invocation.
- **Check settings complexity**: SM supports 9 check types (http, ping, tcp, dns,
  traceroute, multihttp, scripted, grpc, browser) with hundreds of total fields.
  `CheckSettings` uses `map[string]any` for the settings union — preserves
  round-trip fidelity without requiring Go types for every check type variant.

## Not In Scope (Stage 1)

- Check status / timeline (Prometheus queries) — Stage 2
- Tenant info commands — future
- Adding/updating/deleting probes (infrastructure, not user-managed)
- Check alerts management (`/api/v1/check/{id}/alerts`)
- Ad-hoc check execution (`/api/v1/check/adhoc`)
- Token management (`/api/v1/token/*`)
