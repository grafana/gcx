# Go Conventions for Provider Ports

Conventions and linter gotchas discovered during provider migrations.
Reference: `internal/providers/incidents/` for working examples.

## API Group Naming

Use `{name}.ext.grafana.app/v1alpha1` — **singular** name:

```
incident.ext.grafana.app/v1alpha1    ✓
slo.ext.grafana.app/v1alpha1         ✓
alerting.ext.grafana.app/v1alpha1    ✓

incidents.ext.grafana.app            ✗ (plural)
irm-incidents.grafana.app            ✗ (no .ext, product prefix)
```

## Struct Tags

**`omitzero` not `omitempty` for struct-typed fields.** Go 1.24+ `omitempty`
has no effect on structs. The `modernize` linter enforces this. Common case:
custom time types wrapping `time.Time`.

```go
// ✗ omitempty has no effect — FlexTime is a struct
CreatedTime FlexTime `json:"createdTime,omitempty"`

// ✓ omitzero correctly omits zero-valued structs
CreatedTime FlexTime `json:"createdTime,omitzero"`
```

## Linter Traps

**`errchkjson`** — requires checking `json.Marshal` return even for static
maps. In init-time code, use panic:

```go
b, err := json.Marshal(schema)
if err != nil {
    panic(fmt.Sprintf("incidents: failed to marshal schema: %v", err))
}
```

**`testpackage`** — test files must use `package {name}_test`. This means
table codecs need to be exported (`IncidentTableCodec`, not
`incidentTableCodec`) for tests to construct them.

**`nestif`** — complex nested ifs trigger this. Extract helper functions
(e.g., `resolveSchema()` from a nested if-else chain).

**`gci`** — import ordering and struct field alignment. Run `gci diff` to
see what it wants. Common issue: inconsistent spacing before struct tags.

## Lint Compliance Checklist

Phase 0 fills this out per provider. Every applicable rule must be listed with
its section reference before proceeding to Phase 1.

| Document | Section | Applicable? | Notes |
|----------|---------|-------------|-------|
| `CONSTITUTION.md` | CLI Grammar | Yes / No / Partial | {which rules apply to this provider's command tree} |
| `CONSTITUTION.md` | Output Conventions | Yes / No / Partial | {format defaults, codec requirements} |
| `docs/design/naming.md` | Naming | Yes / No / Partial | {noun choice, alias conventions} |
| `docs/design/output.md` | Output Formats | Yes / No / Partial | {table/wide/json/yaml compliance} |
| `docs/design/exit-codes.md` | Exit Codes | Yes / No / Partial | {error handling conventions} |
| `docs/reference/provider-guide.md` | Provider Interface | Yes / No / Partial | {registration, ConfigKeys, TypedCRUD} |
| `docs/reference/provider-guide.md` | Adapter Wiring | Yes / No / Partial | {GVK, aliases, schema/example} |
| `docs/reference/provider-discovery-guide.md` | API Discovery | Yes / No / Partial | {auth model, endpoint patterns, pagination} |
| `docs/reference/provider-discovery-guide.md` | Design Decisions | Yes / No / Partial | {adapter vs provider-only, resource modeling} |

## Debug Logging

Use `log/slog` for debug-level logging in provider clients. Debug logs help
diagnose issues during smoke tests without cluttering normal output.

```go
slog.Debug("fleet: listing pipelines", "url", c.baseURL+"/pipelines")
slog.Debug("k6: token exchange complete", "orgID", orgID, "stackID", stackID)
slog.Debug("oncall: auto-discovered URL", "url", onCallURL, "source", source)
```

**Guidelines:**
- Prefix with provider name: `slog.Debug("{provider}: {action}", ...)`
- Log request URLs and key parameters at Debug level
- Log auth resolution decisions (which fallback was used)
- Never log tokens or secrets — log presence only: `"hasToken", token != ""`
- Enable with `GCX_LOG_LEVEL=debug` or `--log-level=debug`

## Build Commands

```bash
GCX_AGENT_MODE=false make all    # REQUIRED — agent mode changes default
                                         # output formats, producing wrong docs
GCX_AGENT_MODE=false make lint   # after agent phases
```

## Schema + Example Registration

Add `Schema` and `Example` to `adapter.Registration` in `init()`:

```go
adapter.Register(adapter.Registration{
    Factory:    NewAdapterFactory(loader),
    Descriptor: staticDescriptor,
    Aliases:    staticAliases,
    GVK:        staticDescriptor.GroupVersionKind(),
    Schema:     resourceSchema(),   // json.RawMessage
    Example:    resourceExample(),  // json.RawMessage
})
```

**Schema**: static `map[string]any` with JSON Schema structure. Include
`apiVersion` (const), `kind` (const), `metadata`, and `spec` with key
user-facing fields. No external dependencies needed.

**Example**: static `map[string]any` matching gcx's `Example{Resource}()`
output. Include realistic field values — this is what users see when they
run `gcx resources examples {alias}`.
