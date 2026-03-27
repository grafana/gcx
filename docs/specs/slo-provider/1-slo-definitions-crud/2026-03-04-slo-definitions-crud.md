# Stage 1: SLO Definitions CRUD

Parent: [SLO Provider Plan](../2026-03-04-slo-provider-plan.md)

## Context

Foundation stage — all subsequent work depends on this. Establishes the SLO provider, HTTP client, K8s envelope adapter, and CRUD commands for SLO definitions.

~1,300 LOC estimated.

## Prior Art: Terraform Provider

The [terraform-provider-grafana](https://github.com/grafana/terraform-provider-grafana) SLO resource
(`internal/resources/slo/`) uses an OpenAPI-generated client from
[`github.com/grafana/slo-openapi-client/go/slo`](https://github.com/grafana/slo-openapi-client).

**Decision: hand-roll the HTTP client** rather than import the generated client.

Rationale:
- gcx's established pattern is `rest.HTTPClientFor()` + direct HTTP calls
  (see `internal/query/prometheus/client.go`). Consistency matters.
- Generated types use `SloV00Slo`, `*string` optional fields, getter/setter methods —
  poor fit for our `encoding/json` round-trip adapter.
- The SLO API surface is small (4 endpoints). A hand-rolled client is ~200 LOC.
- The generated client would still be useful as a **type reference** during implementation.

Key details extracted from the Terraform provider and OpenAPI spec:
- **Plugin proxy base path**: `/api/plugins/grafana-slo-app/resources`
- **API endpoints** (appended to base path):
  - `GET    /v1/slo`        → list all SLOs
  - `POST   /v1/slo`        → create SLO
  - `GET    /v1/slo/{uuid}` → get single SLO
  - `PUT    /v1/slo/{uuid}` → update SLO
  - `DELETE /v1/slo/{uuid}` → delete SLO
- **Auth**: `Authorization: Bearer <service-account-token>` header (same token as Grafana API)
- **Reports API** (for Stage 2) uses the same base path: `/v1/report`, `/v1/report/{id}`

## New Files

### `internal/providers/slo/definitions/`

| File | Purpose | LOC |
|------|---------|-----|
| `types.go` | Go types: `Slo`, `Query` (6 types incl. failureRatio + grafanaQueries), `Objective`, `Label`, `Alerting`, `ReadOnly`, response wrappers | ~120 |
| `client.go` | HTTP client: List, Get, Create, Update, Delete via `/api/plugins/grafana-slo-app/resources/v1/slo`. Uses `rest.HTTPClientFor()` pattern from `internal/query/prometheus/client.go` | ~200 |
| `client_test.go` | Unit tests with `httptest.Server` | ~200 |
| `adapter.go` | `ToResource`/`FromResource` K8s envelope translation | ~150 |
| `adapter_test.go` | Round-trip property tests | ~150 |
| `commands.go` | definitions group + list/get/push/pull/delete subcommands | ~350 |

### `internal/providers/slo/`

| File | Purpose | LOC |
|------|---------|-----|
| `provider.go` | `SLOProvider` implementing `providers.Provider` interface, wires definitions + (later) reports | ~80 |
| `provider_test.go` | Interface contract tests | ~60 |

## Modified Files

- `internal/providers/registry.go`: add `&slo.SLOProvider{}` to `All()`

## Go Type Definitions

Reference types for `definitions/types.go`. Field names use camelCase matching the API — this ensures lossless `pull -> edit -> push` round-trips.

```go
package definitions

// Slo represents a Grafana SLO definition.
type Slo struct {
    UUID                   string                  `json:"uuid,omitempty"`
    Name                   string                  `json:"name"`
    Description            string                  `json:"description"`
    Query                  Query                   `json:"query"`
    Objectives             []Objective             `json:"objectives"`
    Labels                 []Label                 `json:"labels,omitempty"`
    Alerting               *Alerting               `json:"alerting,omitempty"`
    DestinationDatasource  *DestinationDatasource  `json:"destinationDatasource,omitempty"`
    Folder                 *Folder                 `json:"folder,omitempty"`
    SearchExpression       string                  `json:"searchExpression,omitempty"`
    ReadOnly               *ReadOnly               `json:"readOnly,omitempty"`
}

// Query defines the SLI query. 6 types: freeform, ratio, threshold,
// failureThreshold, failureRatio, grafanaQueries.
// Note: OpenAPI enum only lists 4 — failureRatio and grafanaQueries
// are missing from the spec but present in handler source code.
type Query struct {
    Type                       string          `json:"type"`
    Freeform                   *FreeformQuery  `json:"freeform,omitempty"`
    Ratio                      *RatioQuery     `json:"ratio,omitempty"`
    Threshold                  *ThresholdQuery `json:"threshold,omitempty"`
    FailureThreshold           *ThresholdQuery `json:"failureThreshold,omitempty"`
    GrafanaQueries             []any           `json:"grafanaQueries,omitempty"`
}

type FreeformQuery struct {
    Query string `json:"query"`
}

type RatioQuery struct {
    SuccessMetric  MetricDef `json:"successMetric"`
    TotalMetric    MetricDef `json:"totalMetric"`
    GroupByLabels  []string  `json:"groupByLabels,omitempty"`
}

type ThresholdQuery struct {
    ThresholdExpression        string    `json:"thresholdExpression,omitempty"`
    FailureThresholdExpression string    `json:"failureThresholdExpression,omitempty"`
    Threshold                  Threshold `json:"threshold"`
    GroupByLabels              []string  `json:"groupByLabels,omitempty"`
}

type MetricDef struct {
    PrometheusMetric string `json:"prometheusMetric"`
    Type             string `json:"type,omitempty"`
}

type Threshold struct {
    Value    float64 `json:"value"`
    Operator string  `json:"operator"`
}

type Objective struct {
    Value  float64 `json:"value"`
    Window string  `json:"window"`
}

type Label struct {
    Key   string `json:"key"`
    Value string `json:"value"`
}

type DestinationDatasource struct {
    UID string `json:"uid"`
}

type Folder struct {
    UID string `json:"uid"`
}

type Alerting struct {
    Labels          []Label          `json:"labels,omitempty"`
    Annotations     []Label          `json:"annotations,omitempty"`
    FastBurn        *AlertingRule    `json:"fastBurn,omitempty"`
    SlowBurn        *AlertingRule    `json:"slowBurn,omitempty"`
    AdvancedOptions *AdvancedOptions `json:"advancedOptions,omitempty"`
}

type AlertingRule struct {
    Labels      []Label `json:"labels,omitempty"`
    Annotations []Label `json:"annotations,omitempty"`
}

type AdvancedOptions struct {
    MinFailures int `json:"minFailures,omitempty"`
}

// ReadOnly contains server-generated fields — strip on push.
type ReadOnly struct {
    CreationTimestamp     int64                  `json:"creationTimestamp,omitempty"`
    Status                *Status                `json:"status,omitempty"`
    DrillDownDashboardRef *DashboardRef          `json:"drillDownDashboardRef,omitempty"`
    SourceDatasource      *DestinationDatasource `json:"sourceDatasource,omitempty"`
    Provenance            string                 `json:"provenance,omitempty"`
    ParsesAsRatio         bool                   `json:"parsesAsRatio,omitempty"`
    AllowedActions        []string               `json:"allowedActions,omitempty"`
}

type Status struct {
    Type    string `json:"type"`    // creating|created|updating|updated|deleting|error|unknown
    Message string `json:"message,omitempty"`
}

type DashboardRef struct {
    UID string `json:"UID"`
}

// Response wrappers
type SLOListResponse struct {
    SLOs []Slo `json:"slos"`
}

type SLOCreateResponse struct {
    UUID    string `json:"uuid"`
    Message string `json:"message"`
}

type ErrorResponse struct {
    Code  int    `json:"code"`
    Error string `json:"error"`
}
```

## Envelope Translation

Field mapping for `adapter.go`:

```
SLO API flat JSON              K8s Envelope
------------------              -------------------------
uuid                    --->    metadata.name
name                    --->    spec.name
description             --->    spec.description
query                   --->    spec.query
objectives              --->    spec.objectives
labels                  --->    spec.labels
alerting                --->    spec.alerting
destinationDatasource   --->    spec.destinationDatasource
folder                  --->    spec.folder
searchExpression        --->    spec.searchExpression
readOnly                --->    STRIPPED (not written to file)
```

Added by adapter:
- `apiVersion: slo.ext.grafana.app/v1alpha1`
- `kind: SLO`
- `metadata.namespace`: from config (SLO API doesn't use namespaces)

File path convention: `{output_dir}/SLO/{uuid}.yaml`

### Example YAML

```yaml
apiVersion: slo.ext.grafana.app/v1alpha1
kind: SLO
metadata:
  name: abc123-uuid-here
  namespace: default
spec:
  name: "Checkout Latency SLO"
  description: "99.9% of checkout requests complete in under 500ms"
  destinationDatasource:
    uid: prometheus-prod
  query:
    type: freeform
    freeform:
      query: "sum(rate(probe_success{job='checkout'}[5m]))"
  objectives:
    - value: 0.999
      window: "28d"
  labels:
    - key: team
      value: payments
  folder:
    uid: slo-folder
```

## Push Idempotency

Create-or-update flow for `definitions push`:

1. Read YAML files, `FromResource()` to get `Slo` structs
2. For each SLO: `GET /api/plugins/grafana-slo-app/resources/v1/slo/{uuid}` — if 200 → `PUT` (update), if 404 → `POST` (create)
3. POST accepts `uuid` field for vanity identifiers, so the UUID from `metadata.name` is preserved
4. POST/PUT return 202 Accepted (async) — report success without polling

## Error Handling

```
Status Code → Exit Code Mapping:
  401/403 → exit 3 (auth failure), suggest checking token
  404     → exit 1, suggest `gcx slo definitions list`
  400     → exit 2 (usage error), include API error message
  500     → exit 1 (general error)
  partial → exit 4 (some pushed/deleted, others failed)
```

## RBAC Roles

| Operation | Required Role |
|-----------|--------------|
| List, Get | `sloReader` (Viewer/Editor/Admin) |
| Create, Update | `sloCreator` (Editor/Admin) |
| Delete | `sloDeleter` (Editor/Admin) |

## Implementation Order

1. `definitions/types.go` — all Go types matching OpenAPI schema
2. `definitions/client.go` + `client_test.go` — HTTP client with httptest
3. `definitions/adapter.go` + `adapter_test.go` — round-trip tests prove lossless translation
4. `definitions/commands.go` — CRUD subcommands
5. `provider.go` + `provider_test.go` + `registry.go` — wire it all together

## Table Output

### `slo definitions list`

```
UUID                   NAME                    TARGET   WINDOW   STATUS
abc123def456ghi789jkl  Checkout Latency SLO    99.90%   28d      Created
xyz789ghi012jkl345mno  Auth Availability       99.50%   7d       Updated
pqr012stu345vwx678yza  New Feature SLO         99.00%   28d      Creating
```

## Verification

```bash
make lint && make tests && make build && bin/gcx slo definitions --help
```
