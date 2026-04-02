---
type: bugfix-spec
title: "Fix --json field selection in provider commands"
status: done
beads_id: 322
created: 2026-04-02
---

# Fix --json field selection in provider commands

## Current Behavior

Provider list commands (`gcx slo definitions list`, `gcx synth checks list`, `gcx fleet pipelines list`, `gcx oncall schedules list`, `gcx incidents list`, `gcx k6 projects list`, etc.) accept the `--json` flag without error but silently ignore it. The flag is parsed and validated by `Options.BindFlags()`/`Validate()` — setting `opts.IO.JSONFields` and `opts.IO.JSONDiscovery` correctly — but `Options.Encode()` at `internal/output/format.go:134` never consults these fields. It delegates directly to the plain JSON codec returned by `Codec()`, which outputs the full unfiltered JSON object.

Observable symptoms:

- `gcx slo definitions list --json name` outputs full JSON (all fields) instead of only the `name` field.
- `gcx synth checks list --json ?` outputs full JSON instead of printing available field names and exiting.
- No error message is produced — the flag appears to be accepted but has no effect.

A secondary issue exists in `FieldSelectCodec.Encode()`: its default case calls `toMap()` which unmarshals into `map[string]any`. When provider commands pass a Go slice type (e.g. `[]unstructured.Unstructured`), `json.Unmarshal` into `map[string]any` fails because the JSON representation is an array, not an object. This causes a runtime error instead of field-filtered output.

## Expected Behavior

After the fix:

1. `Options.Encode()` MUST check `opts.JSONDiscovery` and `opts.JSONFields` before delegating to the codec. When `JSONDiscovery` is true, `Encode()` MUST marshal the value to discover its fields via `DiscoverFields()`, print them to the writer, and return without encoding the full value. When `JSONFields` is non-empty, `Encode()` MUST wrap the codec in a `FieldSelectCodec` and encode through it.

2. `FieldSelectCodec.Encode()` MUST handle slice/array input types in its default case. When `toMap()` fails because the JSON representation is an array (not an object), the codec MUST fall back to marshaling the value as a JSON array, unmarshaling into `[]any`, converting each element to `map[string]any`, applying field extraction to each element, and encoding the result as `{"items": [...]}`.

3. Every provider list command that calls `opts.IO.Encode()` MUST automatically gain `--json` field selection and `--json ?` discovery without any per-command code changes.

## Unchanged Behavior

The following MUST remain unaffected:

- **Commands with explicit JSONFields guards**: `cmd/gcx/resources/get.go`, `cmd/gcx/config/command.go`, `cmd/gcx/resources/schemas.go`, and `cmd/gcx/providers/command.go` all check `len(opts.IO.JSONFields) > 0` and return early before reaching `Encode()`. These commands MUST continue to produce identical output. The centralized check in `Encode()` MUST NOT cause double field-selection for these callers.
- **Non-JSON output formats**: `--output yaml`, `--output text`, `--output wide`, and any custom codecs MUST NOT be affected by this change. The `JSONFields`/`JSONDiscovery` logic MUST only activate when the resolved codec format is JSON.
- **Flag parsing and validation**: `Options.BindFlags()`, `Options.Validate()`, and `applyJSONFlag()` MUST NOT change. The `--json` / `--output` mutual exclusion MUST continue to work.
- **Single-object provider commands** (e.g. `gcx slo definitions get <id>`): These MUST also benefit from field selection when `--json` is passed, since they also call `Encode()`.
- **Codec() method**: The public `Codec()` method MUST continue to return the plain format codec (not a field-select codec). Only `Encode()` gains the interception logic.

## Steps to Reproduce

1. Configure gcx with a valid Grafana Cloud context that has SLO definitions: `gcx config use-context <context>`.
2. Run `gcx slo definitions list --json name`.
3. **Observed**: Full JSON output with all fields (name, uuid, description, objectives, etc.).
4. **Expected**: JSON output containing only the `name` field for each item.
5. Run `gcx slo definitions list --json ?`.
6. **Observed**: Full JSON output (same as without the flag).
7. **Expected**: A printed list of available field names, then exit.

## Root Cause Analysis

`Options.Encode()` at `internal/output/format.go:134` is a two-line method:

```go
func (opts *Options) Encode(dst io.Writer, value any) error {
    codec, err := opts.Codec()
    if err != nil {
        return err
    }
    return codec.Encode(dst, value)
}
```

It calls `Codec()` which returns the plain JSON codec based on `opts.OutputFormat`. It never inspects `opts.JSONFields` or `opts.JSONDiscovery`. The four commands that correctly support `--json` all have explicit guards that check these fields and short-circuit before `Encode()` is called. All provider commands lack these guards and depend entirely on `Encode()`.

The secondary issue is in `FieldSelectCodec.Encode()` at `internal/output/field_select.go:67-87`. The default case calls `toMap(value)` which does `json.Marshal` then `json.Unmarshal` into `map[string]any`. When `value` is a slice (e.g. `[]unstructured.Unstructured`), the JSON representation is `[...]` (an array), and unmarshaling an array into `map[string]any` returns an error. The default case has no array fallback.

## Acceptance Criteria

- GIVEN a provider list command (e.g. `gcx slo definitions list`)
  WHEN invoked with `--json name`
  THEN the output MUST be JSON containing only the `name` field for each item

- GIVEN a provider list command (e.g. `gcx synth checks list`)
  WHEN invoked with `--json name,target`
  THEN the output MUST be JSON containing only the `name` and `target` fields for each item

- GIVEN a provider list command
  WHEN invoked with `--json ?`
  THEN the command MUST print available field names (one per line) and exit without printing the full resource data

- GIVEN a provider single-object command (e.g. `gcx slo definitions get <uuid>`)
  WHEN invoked with `--json name`
  THEN the output MUST be JSON containing only the `name` field

- GIVEN `Options.Encode()` is called with `JSONFields` set to `["name"]` and a value of type `[]unstructured.Unstructured`
  WHEN `FieldSelectCodec.Encode()` processes the value
  THEN it MUST produce `{"items": [{"name": ...}, ...]}` instead of returning a marshaling error

- GIVEN `Options.Encode()` is called with `JSONFields` set to `["name"]` and a value of type `map[string]any`
  WHEN `FieldSelectCodec.Encode()` processes the value
  THEN it MUST produce `{"name": ...}` (single object, not wrapped in items)

- GIVEN a command with an explicit JSONFields guard (e.g. `gcx get dashboards --json name`)
  WHEN it checks `len(opts.IO.JSONFields) > 0` and returns early before `Encode()`
  THEN the output MUST be identical to the output before this fix (no double field-selection)

- GIVEN any command invoked with `--output yaml`
  WHEN `Encode()` is called
  THEN JSONFields/JSONDiscovery MUST NOT affect the output; full YAML MUST be produced

- GIVEN any command invoked with `--json name` and `--output json` simultaneously
  WHEN validation runs
  THEN the command MUST succeed (both request JSON, no conflict)

- GIVEN any command invoked with `--json name` and `--output yaml` simultaneously
  WHEN validation runs
  THEN the command MUST return an error indicating `--json requires JSON output`

- The `Options.Codec()` method MUST continue to return the plain format codec, not a FieldSelectCodec. Only `Encode()` MUST apply the field-selection interception.

- Unit tests MUST exist for:
  - `Options.Encode()` with `JSONFields` set (field-filtered output)
  - `Options.Encode()` with `JSONDiscovery` set (field discovery output)
  - `FieldSelectCodec.Encode()` with slice input types (array fallback)
  - `Options.Encode()` with a non-JSON codec and `JSONFields` set (no interference)
