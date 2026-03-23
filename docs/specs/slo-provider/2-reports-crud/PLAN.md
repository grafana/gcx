# Implementation Plan: Stage 2 -- Reports CRUD

Bead: `grafanactl-experiments-mwf`
Branch: `radiohead/slo-provider-plan`
Design: `docs/designs/slo-provider/2-reports-crud/2026-03-04-reports-crud.md`

## 1. Approach Selection

**Approach: Mirror-and-adapt from Stage 1 (definitions/)**

Stage 1 established a proven 5-file subpackage pattern (types, client, adapter,
commands, plus test files). Stage 2 uses the exact same structure with a simpler
data model. The implementation is a mechanical copy-and-adapt of `internal/slo/definitions/`
into `internal/slo/reports/`, changing:

- Types from Slo to Report
- API path from `/v1/slo` to `/v1/report`
- Kind from `SLO` to `Report`
- Response wrapper from `{ slos: [...] }` to `{ reports: [...] }`
- Table columns from UUID/NAME/TARGET/WINDOW/STATUS to UUID/NAME/TIME_SPAN/SLOS

**Why this approach**: Zero design ambiguity. Every decision has a precedent in the
existing definitions/ package. The parallel structure also makes future maintenance
trivial -- anyone who understands one subpackage instantly understands the other.

## 2. Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Data model mismatch with actual API | Low | Medium | Types are taken from design doc which was derived from actual SLO plugin source code |
| Import cycle from new package | Low | Low | reports/ follows same import pattern as definitions/ -- no cross-references |
| Table codec timeSpan display | Low | Low | Design doc shows abbreviated form; implement a simple string map |
| Create response differs from definitions | Low | Low | Both use `{ message, uuid }` per design doc |

No high-risk items. This is a straightforward additive change.

## 3. Task Breakdown

### Step 1: Create `internal/slo/reports/types.go` (~60 LOC)

New file. Package `reports`.

Types to define:
```
Report          -- uuid, name, description, timeSpan, labels, reportDefinition
ReportDefinition -- slos []ReportSlo
ReportSlo        -- sloUuid string
Label            -- key, value (same structure as definitions.Label but redeclared to avoid cross-package import)

ReportListResponse  -- { reports: []Report }
ReportCreateResponse -- { message, uuid }
ErrorResponse        -- { code, error }
```

JSON tags must match the API exactly:
- `timeSpan` (camelCase)
- `reportDefinition` (camelCase)
- `sloUuid` (camelCase)

### Step 2: Create `internal/slo/reports/client.go` (~100 LOC)

New file. Package `reports`.

Mirror `definitions/client.go` exactly, changing:
- `basePath` = `/api/plugins/grafana-slo-app/resources/v1/report`
- `ErrNotFound` message = `"report not found"`
- `List` decodes `ReportListResponse`, null-guards `.Reports`
- `Get` decodes single `Report`
- `Create` decodes `ReportCreateResponse`
- `Update` accepts `uuid string, report *Report`
- `Delete` unchanged pattern
- `doRequest` and `handleErrorResponse` -- identical implementation

### Step 3: Create `internal/slo/reports/client_test.go` (~100 LOC)

New file. Package `reports_test`.

Mirror `definitions/client_test.go`:
- `newTestClient` helper (same pattern)
- `writeJSON` helper (same pattern)
- Table-driven tests for List, Get, Create, Update, Delete
- `TestClient_ErrorResponses` for 401/403/500
- All tests use `t.Context()` and `httptest.NewServer`
- Assert correct URL paths: `/api/plugins/grafana-slo-app/resources/v1/report`

Test cases per method:
- **List**: success with items, empty list, null reports field, server error
- **Get**: success, not found (ErrNotFound sentinel)
- **Create**: success 202, success 200, 400 bad request
- **Update**: success 202, success 200, not found
- **Delete**: success 204, success 200, not found
- **ErrorResponses**: 401, 403, 500

### Step 4: Create `internal/slo/reports/adapter.go` (~80 LOC)

New file. Package `reports`.

Constants:
```go
const (
    APIVersion = "slo.ext.grafana.app/v1alpha1"  // same as definitions
    Kind       = "Report"                          // different from definitions
)
```

Functions:
- `ToResource(report Report, namespace string) (*resources.Resource, error)`
  - Marshal report to JSON, unmarshal to map
  - `delete(specMap, "uuid")` -- strip server-managed field
  - Build K8s envelope: apiVersion, kind, metadata (name=uuid, namespace), spec
  - Return `resources.MustFromObject(obj, resources.SourceInfo{})`

- `FromResource(res *resources.Resource) (*Report, error)`
  - Extract `spec` from `res.Object.Object`
  - Marshal spec to JSON, unmarshal to `Report`
  - Restore UUID from `res.Raw.GetName()`

- `FileNamer(outputFormat string) func(*resources.Resource) string`
  - Returns `Report/{name}.{format}` (not `SLO/`)

### Step 5: Create `internal/slo/reports/adapter_test.go` (~60 LOC)

New file. Package `reports_test`.

Test helpers:
- `minimalReport()` -- uuid, name, description, timeSpan, reportDefinition with one SLO
- `fullReport()` -- all fields populated including labels

Tests:
- `TestToResource_MinimalReport` -- checks APIVersion, Kind, Name, Namespace, spec fields
- `TestToResource_MapsUUIDToMetadataName` -- UUID in metadata.name, not in spec
- `TestToResource_SetsCorrectGVK` -- group=slo.ext.grafana.app, version=v1alpha1, kind=Report
- `TestFromResource_RestoresUUID` -- round-trip UUID preservation
- `TestRoundTrip_Report` -- full round-trip: Report -> Resource -> Report
- `TestFileNamer` -- `Report/test-uuid.yaml` and `Report/test-uuid.json`

### Step 6: Create `internal/slo/reports/commands.go` (~200 LOC)

New file. Package `reports`.

Structure mirrors `definitions/commands.go` exactly:

```go
// RESTConfigLoader redeclared locally to avoid import cycle
type RESTConfigLoader interface {
    LoadRESTConfig(ctx context.Context) (config.NamespacedRESTConfig, error)
}

func Commands(loader RESTConfigLoader) *cobra.Command
```

Commands:
- **list** -- `slo reports list`
  - Table codec: UUID, NAME, TIME_SPAN, SLOS columns
  - TIME_SPAN mapping: weeklySundayToSunday->"weekly", calendarMonth->"monthly", calendarYear->"yearly"
  - SLOS column: `len(report.ReportDefinition.Slos)`
  - Non-table formats: convert to K8s envelope via ToResource

- **get** -- `slo reports get UUID`
  - Single report, YAML default format
  - Convert to K8s envelope

- **pull** -- `slo reports pull`
  - Output dir flag `-d`, default "."
  - Subdirectory: `Report/` (not `SLO/`)
  - File pattern: `{uuid}.yaml`
  - Success message: "Pulled %d reports to %s/"

- **push** -- `slo reports push FILE...`
  - `--dry-run` flag
  - Read YAML files, decode to unstructured, convert via FromResource
  - Upsert logic: check existence by UUID, create or update
  - Same upsertReport helper pattern as definitions' upsertSLO

- **delete** -- `slo reports delete UUID...`
  - `--force` flag to skip confirmation
  - Confirmation prompt: "Delete %d report(s)? [y/N]"

### Step 7: Modify `internal/slo/provider.go`

Add import and wire reports subcommand:

```go
import "github.com/grafana/grafanactl/internal/slo/reports"

// In Commands():
sloCmd.AddCommand(reports.Commands(loader))
```

### Step 8: Update `internal/slo/provider_test.go`

Add test assertions for the reports subcommand:

```go
// In TestSLOProvider_Commands:
// Find reports subcommand
var reportsCmd *cobra.Command
for _, sub := range sloCmd.Commands() {
    if sub.Name() == "reports" {
        reportsCmd = sub
        break
    }
}
require.NotNil(t, reportsCmd, "expected 'reports' subcommand")

// Check all expected subcommands exist
reportSubNames := make([]string, 0, len(reportsCmd.Commands()))
for _, sub := range reportsCmd.Commands() {
    reportSubNames = append(reportSubNames, sub.Name())
}
assert.Contains(t, reportSubNames, "list")
assert.Contains(t, reportSubNames, "get")
assert.Contains(t, reportSubNames, "push")
assert.Contains(t, reportSubNames, "pull")
assert.Contains(t, reportSubNames, "delete")
```

### Step 9: Verification

```bash
make lint && make tests && make build
bin/grafanactl slo reports --help
bin/grafanactl slo --help    # verify both definitions and reports appear
```

## 4. Test Strategy

| File | Test Type | Coverage |
|------|-----------|----------|
| `reports/client_test.go` | Unit (httptest) | All 5 CRUD methods + error responses |
| `reports/adapter_test.go` | Unit | ToResource, FromResource, round-trip, FileNamer |
| `provider_test.go` | Unit | Reports subcommand registration, all 5 sub-subcommands |

All tests are:
- **Table-driven** per project convention
- Use **`t.Context()`** for context
- Use **`httptest.NewServer`** per test case (not shared)
- Use **`require`/`assert`** from testify
- Package `reports_test` (external test package)

No integration tests needed -- this is a pure client/adapter/CLI layer with no external dependencies.

## 5. File Summary

| File | Action | LOC (est) |
|------|--------|-----------|
| `internal/slo/reports/types.go` | Create | ~60 |
| `internal/slo/reports/client.go` | Create | ~100 |
| `internal/slo/reports/client_test.go` | Create | ~100 |
| `internal/slo/reports/adapter.go` | Create | ~80 |
| `internal/slo/reports/adapter_test.go` | Create | ~60 |
| `internal/slo/reports/commands.go` | Create | ~200 |
| `internal/slo/provider.go` | Modify | +3 lines |
| `internal/slo/provider_test.go` | Modify | +15 lines |

**Total estimated**: ~620 LOC (slightly above the 500 estimate due to test thoroughness)

## 6. Implementation Order

The steps MUST be executed in this order due to compile dependencies:

```
types.go          (no deps within reports/)
    |
    v
client.go         (depends on types.go)
    |
    v
adapter.go        (depends on types.go + resources package)
    |
    v
client_test.go    (depends on client.go + types.go)
adapter_test.go   (depends on adapter.go + types.go)
    |              [these two can be parallel]
    v
commands.go       (depends on client.go + adapter.go + types.go)
    |
    v
provider.go       (depends on reports package)
provider_test.go  (depends on provider.go)
    |
    v
make lint && make tests && make build
```

## 7. Complexity

**LOW**

Rationale: This is a mechanical mirror of an existing, well-tested pattern. No
new architectural decisions, no new dependencies, no cross-cutting concerns. The
data model is simpler than definitions (fewer types, no nested query structures).
The only creative work is the timeSpan display mapping in the table codec.

## 8. Recommended Model

**Sonnet**

This is purely mechanical copy-and-adapt work. Sonnet can follow the Stage 1
template precisely. Opus would be overkill for this task.

## 9. Human Review Checkpoints

- **Human review after planning**: NO -- the plan is a direct mirror of an
  established pattern with no ambiguity
- **Human review after implementation**: YES -- verify the timeSpan mapping,
  table output format, and that all commands wire correctly before merging
