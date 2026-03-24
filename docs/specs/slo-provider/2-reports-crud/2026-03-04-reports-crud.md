# Stage 2: Reports CRUD

Parent: [SLO Provider Plan](../2026-03-04-slo-provider-plan.md)
Depends on: [Stage 1: SLO Definitions CRUD](../1-slo-definitions-crud/2026-03-04-slo-definitions-crud.md)

## Context

Extends the SLO provider with report management. Separate subpackage, parallel structure to definitions.

~500 LOC estimated.

## New Files

### `internal/providers/slo/reports/`

| File | Purpose | LOC |
|------|---------|-----|
| `types.go` | `Report`, `ReportDefinition`, `ReportSlo`, response wrappers | ~60 |
| `client.go` | Report HTTP methods (same base, `/v1/report`) | ~100 |
| `client_test.go` | Report client tests | ~100 |
| `adapter.go` | Report `ToResource`/`FromResource` | ~80 |
| `adapter_test.go` | Report round-trip tests | ~60 |
| `commands.go` | `slo reports` subcommand group with list/get/push/pull/delete | ~200 |

## Modified Files

- `internal/providers/slo/provider.go` — add reports subcommand to `slo` parent

## Report Data Model

```yaml
Report:
  uuid:             string         # auto-generated nanoid (21 chars, lowercase alphanumeric)
  name:             string         # required, max MAX_NAME_LENGTH
  description:      string         # required, max MAX_DESC_LENGTH
  timeSpan:         string         # enum: weeklySundayToSunday | calendarMonth | calendarYear
  labels:           Label[]        # optional, same {key, value} pair type as SLOs
  reportDefinition:                # required
    slos:           ReportSlo[]    # list of {sloUuid: string}, at least one required
```

### Validation Rules (from source code)

- Name is required, max length shared with SLOs
- Description max length shared with SLOs
- Report definition must contain at least one SLO
- All referenced SLO UUIDs must exist (server validates on create/update)
- UUID is 21 characters, lowercase alphanumeric only

### API Endpoints

| Method | Path | Response Code | Response Body |
|--------|------|---------------|---------------|
| `GET` | `/v1/report` | 200 | `{ reports: [...] }` |
| `POST` | `/v1/report` | 202 | `{ message, uuid }` |
| `GET` | `/v1/report/{id}` | 200 | `Report` object |
| `PUT` | `/v1/report/{id}` | 202 | empty |
| `DELETE` | `/v1/report/{id}` | 204 | empty |

### Limitations

Only event-based (ratio) SLOs are supported in reports. The CUE schema includes a commented-out `type: "ratio" | "mixed"` field suggesting future mixed-type support.

## Key Differences from SLO Definitions

- `Kind: Report` (vs `Kind: SLO`)
- File path: `Report/{uuid}.yaml`
- Response wrapper uses `{ reports: [...] }` (vs `{ slos: [...] }`)
- Data model is simpler: name, description, timeSpan, labels, reportDefinition (list of SLO UUIDs)

## Future Capabilities (from CUE schema)

Planned features that aren't active yet — design types with pointer fields to accommodate these without breaking changes:

- **Weighted SLOs**: `weight?: number` on `ReportSlo` — non-equal weighting of SLOs in combined metrics
- **Label-based report definitions**: `#ReportDefinitionLabels` with label selectors — dynamic SLO inclusion by labels instead of explicit UUID lists
- **Report type field**: `type: "ratio" | "mixed"` — currently only ratio SLOs

## Table Output

### `slo reports list`

```
UUID                   NAME                    TIME_SPAN       SLOS
abc123def456ghi789jkl  Weekly Platform Report   weekly          3
xyz789ghi012jkl345mno  Monthly Checkout SLOs    monthly         7
```

## Verification

```bash
make lint && make tests && make build && bin/grafanactl slo reports --help
```
