# kg diagnose — Output Format Cleanup

**Request:** Maintainer wants kg diagnose output cleaned up for readability, using table format like kg entities.

## Current State

| Codec | File | Tables | Library |
|---|---|---|---|
| `DiagnoseTextCodec` | diagnose.go:869 | 1 (CHECK/STATUS/DETAIL) | raw tabwriter |
| `ServiceDiagnoseTextCodec` | diagnose_service.go:483 | 1 (CHECK/STATUS/DETAIL) | raw tabwriter |
| `LabelsDiagnoseTextCodec` | diagnose_labels.go:357 | 2 (checks + mapping) | raw tabwriter |

`kg entities` uses `style.NewTable()` from `internal/style/table.go` — lipgloss borders/colors in TTY, plain tabwriter fallback for pipes/agents.

## Plan

Replace raw `tabwriter.NewWriter` calls in the 3 codecs with `style.NewTable()`. Non-table sections (ENTITY info, RELATIONSHIPS, Recommendations, Diagnosis, Next Steps) stay as-is.

## Predicted Problems

1. **Long DETAIL strings** — lipgloss tables have fixed total Width. Need `.ColumnWidths()` to pin CHECK and STATUS narrow, give DETAIL the rest.
2. **Tests assert specific output** — 3 test functions use `assert.Contains` (good, not exact match). Plain-mode tabwriter flags differ slightly (`TabIndent|DiscardEmptyColumns` vs none) — could shift whitespace.
3. **Import changes** — need `style` package, can remove `text/tabwriter` only if no other usage in the file. `diagnose.go` may still use tabwriter elsewhere.
4. **Mapping table (labels)** — STATUS column has long values like `"not mapped — check relabeling rules"`. Needs ColumnWidths.
5. **`IsStylingEnabled()` in tests** — tests run without TTY → `renderPlain()` used → slightly different tabwriter flags may affect assertions.

## Files to Change

- `internal/providers/kg/diagnose.go` — DiagnoseTextCodec.Encode
- `internal/providers/kg/diagnose_service.go` — ServiceDiagnoseTextCodec.Encode
- `internal/providers/kg/diagnose_labels.go` — LabelsDiagnoseTextCodec.Encode
- `internal/providers/kg/diagnose_test.go` — verify/fix assertions

## Risk

Low. `style.NewTable` plain mode produces nearly identical output to raw tabwriter. Main risk is whitespace changes breaking test assertions.
