# ADR: Align `resources examples` with `resources schemas` UX

**Date**: 2026-03-25
**Status**: Accepted (implemented)

## Context

`resources schemas` supports listing all resource types (no args) or filtering by selector,
with text/wide/json/yaml output formats. `resources examples` required exactly one argument
and returned the raw example manifest directly. This inconsistency made the two sibling
commands behave differently for no good reason.

## Decision

Align `resources examples` with `resources schemas`:

| Aspect | Before | After |
|--------|--------|-------|
| Args | Exactly 1 required | 0 or 1 (optional) |
| Default format | `yaml` | `text` (tabular) |
| No-arg behavior | Error | List all resources with examples |
| Single-resource output | Raw manifest | Nested `group -> version -> [{kind, example}]` |
| Resources without examples | N/A | Skipped (same as schemas skips no-schema resources) |

### Output structure (json/yaml)

```
{
  "<group>": {
    "<version>": [
      {
        "kind": "<Kind>",
        "plural": "<plural>",
        "singular": "<singular>",
        "example": { <full example manifest> }
      }
    ]
  }
}
```

### Breaking change

The single-resource output shape changes from a raw manifest to the nested structure.
This is acceptable because:
- The command is relatively new and not widely scripted against
- Consistency with `schemas` outweighs backward compatibility here
- Users needing the raw manifest can use `jq` to extract it

## Alternatives Considered

1. **Nested only for list-all, raw for single-resource** — Non-breaking but inconsistent
   output shapes depending on whether an arg is passed. Rejected for added complexity and
   surprising behavior.

2. **Keep yaml default, add list-all** — Would diverge further from schemas. Rejected
   for inconsistency.

## Consequences

- `resources examples` and `resources schemas` now have identical UX patterns
- Default output is a concise table showing which resources have examples
- Structured output (json/yaml) uses the same nested group/version layout as schemas
- Reuses the existing `tabCodec` from schemas for tabular output
