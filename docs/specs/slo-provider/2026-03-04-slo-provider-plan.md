# SLO Provider Implementation Plan

## Context

grafanactl needs an SLO provider as the first reference implementation of the provider plugin system (Wave 1, `grafanactl-experiments-htx`). The SLO API is a plugin-hosted REST API at `/api/plugins/grafana-slo-app/resources/v1/slo` — it's the simplest Grafana Cloud product to integrate: same auth, clean CRUD, no extra credentials needed.

The research phase produced three documents covering the full API surface (18 endpoints), recording rule architecture, Reports CRUD, graph package reuse, and command design. This plan synthesizes those into an implementation roadmap.

Source bead: `grafanactl-experiments-htx`

## Key Design Decisions

### 1. Token: Reuse `grafana.token` by default

The SLO plugin API uses the same Grafana service account token as dashboard operations. Rather than forcing a separate `providers.slo.token`, the provider should:

- Declare `ConfigKeys()` as empty (matching the bead's acceptance criteria)
- Read token from `curCtx.Grafana.Token` directly

This matches user expectation: one token, all Grafana features.

### 2. apiVersion: `slo.ext.grafana.app/v1alpha1`

Internal convention for grafanactl's K8s envelope format. Uses the `.ext.grafana.app` suffix to signal this is an extension product (not core App Platform). SLOs use `Kind: SLO`, Reports use `Kind: Report`, both under the same group.

### 3. Plugin API only (not K8s API)

The K8s-compatible APIs (`/apis/grafana-slo-app.plugins.grafana.com/...`) are NOT accessible in Grafana Cloud. All access goes through the plugin resource API at `/api/plugins/grafana-slo-app/resources/v1/`.

### 4. HTTP client: plain `http.Client` with Bearer token

Not using `rest.HTTPClientFor` since this isn't a K8s API. The client constructs requests with `Authorization: Bearer <token>` against `{grafana.server}/api/plugins/grafana-slo-app/resources/v1/slo`.

## Command Surface

```
grafanactl slo                                  Manage Grafana SLOs
├── definitions                                 Manage SLO definitions
│   ├── list                                    List SLO definitions
│   ├── get <uuid>                              Get a specific SLO by UUID
│   ├── push [path...]                          Push local SLO definitions to Grafana
│   ├── pull                                    Pull SLO definitions to local files
│   ├── delete <uuid...>                        Delete SLOs from Grafana
│   └── status [uuid]                           Show SLO performance and health
│       ├── -o table (default)                  NAME, UUID, OBJECTIVE, SLI, BUDGET, STATUS
│       ├── -o wide                             + BURN_RATE, SLI_1H, SLI_1D
│       ├── -o json/yaml                        Full data
│       └── -o graph [--start/--end]            Bar chart (instant) or line chart (range)
└── reports                                     Manage SLO report definitions
    ├── list                                    List SLO reports
    ├── get <uuid>                              Get a specific report by UUID
    ├── push [path...]                          Push local report definitions
    ├── pull                                    Pull report definitions to local files
    ├── delete <uuid...>                        Delete reports from Grafana
    └── status                                  Show report health (combined SLI per report)
        ├── -o table (default)                  NAME, TIME_SPAN, SLOS, COMBINED_SLI, BUDGET, STATUS
        ├── -o wide                             + per-SLO breakdown
        ├── -o json/yaml                        Full data
        └── -o graph                            Bar chart of combined SLI per report
```

**Nesting rationale:** Both definitions and reports are noun groups with parallel CRUD + status. This avoids the awkwardness of `slo list` (what are you listing?) vs `slo reports list`.

**Reports status:** Shows aggregate health per report — fetches report definitions (list of SLO UUIDs), queries `grafana_slo_*` metrics for each referenced SLO, computes combined SLI and error budget. More complex than definition status (fan-out across SLOs) but same hybrid pattern.

## Implementation Stages

Four stages, each with a shippable deliverable. See individual stage documents for full detail:

- [Stage 1: SLO Definitions CRUD](1-slo-definitions-crud/2026-03-04-slo-definitions-crud.md) (~1,300 LOC)
- [Stage 2: Reports CRUD](2-reports-crud/2026-03-04-reports-crud.md) (~500 LOC)
- [Stage 3: SLO Definitions Status](3-definitions-status/2026-03-04-definitions-status.md) (~350 LOC)
- [Stage 4: Reports Status](4-reports-status/2026-03-04-reports-status.md) (~200 LOC)

## Dependency Graph

```
Stage 1: SLO Definitions CRUD
    │
    ├──► Stage 2: Reports CRUD (needs provider.go parent command)
    │        │
    │        └──► Stage 4: Reports Status (needs report client + status patterns)
    │
    └──► Stage 3: SLO Definitions Status (needs SLO client + types)
             │
             └──► Stage 4: Reports Status (needs chart converters + status patterns)
```

Stages 2 and 3 can run in parallel after Stage 1. Stage 4 requires both.

## File Tree (all stages)

```
internal/providers/slo/
├── provider.go              # Stage 1: Provider interface impl + command wiring
├── provider_test.go         # Stage 1: contract tests
├── chart.go                 # Stage 3: shared SLO graph converters
├── definitions/
│   ├── types.go             # Stage 1: SLO types
│   ├── client.go            # Stage 1: SLO HTTP client
│   ├── client_test.go       # Stage 1: SLO client tests
│   ├── adapter.go           # Stage 1: SLO envelope adapter
│   ├── adapter_test.go      # Stage 1: adapter round-trip tests
│   ├── commands.go          # Stage 1: definitions CRUD commands
│   ├── status.go            # Stage 3: definitions status command
│   └── status_test.go       # Stage 3: definition status tests
└── reports/
    ├── types.go             # Stage 2: Report types
    ├── client.go            # Stage 2: Report HTTP client
    ├── client_test.go       # Stage 2: report client tests
    ├── adapter.go           # Stage 2: Report envelope adapter
    ├── adapter_test.go      # Stage 2: report round-trip tests
    ├── commands.go          # Stage 2: reports CRUD commands
    ├── status.go            # Stage 4: reports status command
    └── status_test.go       # Stage 4: report status tests

internal/providers/registry.go  # Stage 1: one-line change
internal/graph/chart.go         # Stage 3: optional threshold support
```

## Table Output Designs

### `slo definitions list`

```
UUID                   NAME                    TARGET   WINDOW   STATUS
abc123def456ghi789jkl  Checkout Latency SLO    99.90%   28d      Created
xyz789ghi012jkl345mno  Auth Availability       99.50%   7d       Updated
pqr012stu345vwx678yza  New Feature SLO         99.00%   28d      Creating
```

### `slo definitions status`

```
NAME                    UUID          OBJECTIVE   WINDOW   SLI       BUDGET    STATUS
payment-api-latency     abc123def     99.50%      28d      99.72%    44.0%     OK
checkout-availability   xyz789ghi     99.90%      28d      99.85%    -50.0%    BREACHING
new-feature-slo         pqr012stu     99.50%      28d      --        --        Creating
```

### `slo definitions status -o wide`

```
NAME                  UUID        OBJECTIVE  WINDOW  SLI      BUDGET   BURN_RATE  SLI_1H   SLI_1D   STATUS
payment-api-latency   abc123def   99.50%     28d     99.72%   44.0%    0.8x       99.91%   99.80%   OK
checkout-availability xyz789ghi   99.90%     28d     99.85%   -50.0%   2.3x       99.70%   99.82%   BREACHING
```

### `slo reports list`

```
UUID                   NAME                    TIME_SPAN       SLOS
abc123def456ghi789jkl  Weekly Platform Report   weekly          3
xyz789ghi012jkl345mno  Monthly Checkout SLOs    monthly         7
```

### `slo reports status`

```
NAME                    TIME_SPAN   SLOS   COMBINED_SLI   COMBINED_BUDGET   STATUS
Weekly Platform Report  weekly      3      99.82%         36.0%             OK
Monthly Checkout SLOs   monthly     7      99.71%         -29.0%            BREACHING
```

### `slo reports status -o wide`

```
NAME                    TIME_SPAN   SLOS   COMBINED_SLI   COMBINED_BUDGET   STATUS
Weekly Platform Report  weekly      3      99.82%         36.0%             OK
  payment-api-latency                      99.72%         44.0%             OK
  checkout-avail                           99.85%         -50.0%            BREACHING
  user-auth-errors                         99.42%         42.0%             OK
```

## Full API Route Map (18 endpoints)

All paths relative to `/api/plugins/grafana-slo-app/resources`:

| # | Method | Path | Purpose | In OpenAPI? | RBAC Role |
|---|--------|------|---------|-------------|-----------|
| 1 | GET | `/v1/slo` | List all SLOs | Yes | sloReader |
| 2 | POST | `/v1/slo` | Create SLO | Yes | sloCreator |
| 3 | GET | `/v1/slo/{id}` | Read SLO | Yes | sloReader |
| 4 | PUT | `/v1/slo/{id}` | Update SLO | Yes | sloUpdater |
| 5 | DELETE | `/v1/slo/{id}` | Delete SLO | Yes | sloDeleter |
| 6 | GET | `/v1/report` | List all reports | Yes | sloReader |
| 7 | POST | `/v1/report` | Create report | Yes | sloCreator |
| 8 | GET | `/v1/report/{id}` | Read report | Yes | sloReader |
| 9 | PUT | `/v1/report/{id}` | Update report | Yes | sloCreator |
| 10 | DELETE | `/v1/report/{id}` | Delete report | Yes | sloDeleter |
| 11 | POST | `/v1/slo/eval` | Preview rendered PromQL | No | — |
| 12-16 | — | `/v1/preferences` | Org preferences CRUD | No | orgPref* |
| 17 | POST | `/exp/predict` | Monte Carlo SLI prediction | No | — |
| 18 | POST | `/exp/generate/hcl` | Terraform HCL export | No | — |

Endpoints 1-10 are in scope. Endpoints 11-18 are documented here for reference but not implemented.

## Areas of Uncertainty

- **Token fallback:** Plan assumes `grafana.token` works for SLO API. If not, we add `ConfigKeys()` returning `{Name: "token", Secret: true}` and fallback logic. Low risk — research confirms same SA token.
- **Async creates:** POST returns 202 (accepted, not complete). Push command reports success on 202 without polling. If users need to wait for provisioning, `slo definitions status` shows lifecycle state.
- **No pagination:** GET `/v1/slo` returns all SLOs in a flat array. Fine for expected scale (tens to low hundreds per org). If this becomes a problem, it's an API-side change.
- **Status datasource routing:** Status commands need to query Prometheus metrics against each SLO's `destinationDatasource.uid`. The existing Prom query client uses the datasource proxy API which should work with the main grafana token.
- **Combined SLI for reports:** The Grafana UI computes combined metrics client-side. We'll replicate this logic (weighted average of SLIs based on total event rate, or simple average for freeform SLOs). The exact weighting algorithm may need validation against the UI.

## Not In Scope

- Undocumented/experimental endpoints: `POST /v1/slo/eval`, `POST /exp/predict`, `POST /exp/generate/hcl`, `/v1/preferences`
- Sparklines in table output
- Conditional bar coloring (red/green by SLI vs target)
