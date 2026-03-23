# Pyroscope Query Support for grafanactl

**Date**: 2026-03-06
**Status**: Draft
**Author**: Claude (AI assistant)

## Overview

Add Pyroscope datasource support to grafanactl, enabling profile querying via the
existing `query` command and Pyroscope-specific operations via `datasources pyroscope`
subcommands.

## Goals

1. **Query support**: Execute profile queries against Pyroscope datasources via
   `grafanactl query -t pyroscope`
2. **Profile types discovery**: List available profile types via
   `grafanactl datasources pyroscope profile-types`
3. **Label discovery**: List labels and label values via
   `grafanactl datasources pyroscope labels`

## Non-Goals

- CRUD operations for Pyroscope resources (recording rules, settings, ad-hoc profiles)
- Flame graph rendering in terminal (complex visualization)
- Direct Pyroscope API access (all access via Grafana datasource proxy)

## API Surface

### Pyroscope Connect RPC Endpoints (via Grafana datasource proxy)

All endpoints are accessed via Grafana's datasource proxy:
```
/apis/grafana-pyroscope-datasource.datasource.grafana.app/v0alpha1/namespaces/{ns}/datasources/{uid}/resource/{path}
```

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `querier.v1.QuerierService/ProfileTypes` | POST | List available profile types |
| `querier.v1.QuerierService/LabelNames` | POST | List label names |
| `querier.v1.QuerierService/LabelValues` | POST | List values for a label |
| `querier.v1.QuerierService/SelectMergeStacktraces` | POST | Query merged flame graph |
| `querier.v1.QuerierService/SelectSeries` | POST | Query time series metrics |

### Request/Response Formats

**ProfileTypes Request**:
```json
{"start": "1709740800000", "end": "1709744400000"}
```

**ProfileTypes Response**:
```json
{
  "profileTypes": [
    {"ID": "process_cpu:cpu:nanoseconds:cpu:nanoseconds", "name": "process_cpu", "sampleType": "cpu", "sampleUnit": "nanoseconds"},
    {"ID": "memory:alloc_objects:count:space:bytes", "name": "memory", "sampleType": "alloc_objects", "sampleUnit": "count"}
  ]
}
```

**SelectMergeStacktraces Request**:
```json
{
  "labelSelector": "{service_name=\"frontend\"}",
  "profileTypeID": "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
  "start": "1709740800000",
  "end": "1709744400000",
  "maxNodes": "1024"
}
```

**SelectMergeStacktraces Response**:
```json
{
  "flamegraph": {
    "names": ["total", "main", "runtime.main", "http.ListenAndServe"],
    "levels": [{"values": ["0", "100", "100", "0"]}, ...],
    "total": "100000",
    "maxSelf": "5000"
  }
}
```

## Design Decisions

### 1. Auth Strategy: Reuse `grafana.token`

Access Pyroscope via Grafana's datasource proxy. No separate credentials needed.
- ConfigKeys: `[]` (empty)
- Token source: `curCtx.Grafana.Token`

### 2. API Client: Hand-rolled HTTP client

Follow existing Prometheus/Loki pattern:
- Use `rest.HTTPClientFor()` from k8s client-go
- Direct HTTP POST to Connect RPC endpoints
- JSON encoding (not protobuf)

### 3. Query Output: Table format with top functions

For flame graph results, show top N functions by sample count:
```
FUNCTION                   SELF      TOTAL     PERCENTAGE
runtime.futex             15000     15000     15.00%
net/http.(*conn).serve    12000     45000     12.00%
encoding/json.Unmarshal    8000     20000      8.00%
```

### 4. Config: Add `default-pyroscope-datasource`

Add to `Context` struct for datasource resolution.

## File Changes

### New Files

| File | Purpose | LOC Est. |
|------|---------|----------|
| `internal/query/pyroscope/client.go` | HTTP client for Pyroscope queries | ~150 |
| `internal/query/pyroscope/types.go` | Request/response type definitions | ~80 |
| `internal/query/pyroscope/formatter.go` | Table formatting for profile data | ~60 |
| `cmd/grafanactl/datasources/pyroscope.go` | profile-types, labels subcommands | ~200 |

### Modified Files

| File | Changes |
|------|---------|
| `cmd/grafanactl/query/command.go` | Add `case "pyroscope"` dispatch (~30 lines) |
| `cmd/grafanactl/datasources/command.go` | Register `pyroscopeCmd` (~2 lines) |
| `internal/config/types.go` | Add `DefaultPyroscopeDatasource` field (~2 lines) |

## Command Interface

### `grafanactl query -t pyroscope`

```bash
# Query CPU profile for a service
grafanactl query -t pyroscope -d <pyroscope-uid> \
  -e '{service_name="frontend"}' \
  --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds \
  --start now-1h --end now

# Output top functions as table (default)
grafanactl query -t pyroscope -d <pyroscope-uid> -e '{service_name="frontend"}' --profile-type cpu

# Output as JSON
grafanactl query -t pyroscope -d <pyroscope-uid> -e '{service_name="frontend"}' -o json
```

New flags for pyroscope queries:
- `--profile-type`: Profile type ID (required for pyroscope)
- `--max-nodes`: Maximum nodes in flame graph (default 1024)

### `grafanactl datasources pyroscope profile-types`

```bash
# List available profile types
grafanactl datasources pyroscope profile-types -d <pyroscope-uid>

# Output:
# ID                                              NAME          SAMPLE_TYPE    UNIT
# process_cpu:cpu:nanoseconds:cpu:nanoseconds    process_cpu   cpu            nanoseconds
# memory:alloc_objects:count:space:bytes         memory        alloc_objects  count
```

### `grafanactl datasources pyroscope labels`

```bash
# List all labels
grafanactl datasources pyroscope labels -d <pyroscope-uid>

# Get values for a label
grafanactl datasources pyroscope labels -d <pyroscope-uid> --label service_name
```

## Implementation Plan

### Stage 1: Core Infrastructure (~150 LOC)

1. Create `internal/query/pyroscope/types.go` with request/response types
2. Create `internal/query/pyroscope/client.go` with HTTP client
3. Add unit tests with httptest

### Stage 2: Datasource Commands (~200 LOC)

1. Create `cmd/grafanactl/datasources/pyroscope.go`
   - `profile-types` command
   - `labels` command
2. Register in `command.go`
3. Add `DefaultPyroscopeDatasource` to config

### Stage 3: Query Integration (~100 LOC)

1. Add `case "pyroscope"` to `cmd/grafanactl/query/command.go`
2. Create `internal/query/pyroscope/formatter.go` for table output
3. Add `--profile-type` and `--max-nodes` flags

### Verification Checklist

- [ ] `make build` succeeds
- [ ] `make tests` passes
- [ ] `make lint` passes
- [ ] `grafanactl query -t pyroscope --help` shows pyroscope options
- [ ] `grafanactl datasources pyroscope profile-types -d <uid>` works
- [ ] `grafanactl datasources pyroscope labels -d <uid>` works
- [ ] JSON/YAML output works for all commands
- [ ] Table output shows meaningful profile data

## Appendix: Type Definitions

```go
// internal/query/pyroscope/types.go

type QueryRequest struct {
    LabelSelector string
    ProfileTypeID string
    Start         time.Time
    End           time.Time
    MaxNodes      int64
}

type QueryResponse struct {
    Flamegraph *Flamegraph `json:"flamegraph,omitempty"`
}

type Flamegraph struct {
    Names   []string `json:"names"`
    Levels  []Level  `json:"levels"`
    Total   int64    `json:"total,string"`
    MaxSelf int64    `json:"maxSelf,string"`
}

type Level struct {
    Values []int64 `json:"values"`
}

type ProfileTypesResponse struct {
    ProfileTypes []ProfileType `json:"profileTypes"`
}

type ProfileType struct {
    ID         string `json:"ID"`
    Name       string `json:"name"`
    SampleType string `json:"sampleType"`
    SampleUnit string `json:"sampleUnit"`
    PeriodType string `json:"periodType"`
    PeriodUnit string `json:"periodUnit"`
}

type LabelNamesResponse struct {
    Names []string `json:"names"`
}

type LabelValuesResponse struct {
    Names []string `json:"names"` // Pyroscope uses "names" for both
}
```
