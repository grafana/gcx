---
type: feature-plan
title: "KG thresholds — CRUD over v1 threshold config (read-only first)"
status: draft
spec: # this plan is the design of record
created: 2026-07-28
issue: https://github.com/grafana/gcx/issues/1078
---

# Design: `gcx kg thresholds`

> **Date**: 2026-07-28
> **Status**: draft
> **Implements**: issue #1078 — CRUD over the **v1** Asserts threshold config API
> (`/v1/config/threshold-rule(s)`). Target v1, not v2 — the Asserts plugin UI and
> all real user config run on v1; v2 is unadopted.
> **Scope of this branch**: **read-only first** — `list` + `get`. Writes (`set`,
> `upsert`, `delete`) land in a follow-up once the read shapes are proven live.
> **Owner**: Kevin Blaschke.

## Problem

There is no gcx surface for Asserts threshold rules. Users configuring thresholds
today do it only through the Asserts app UI (`ManageAssertions.service.ts` → v1
`/config/threshold-rule(s)`). gcx should expose the same config for scripted reads
(and later writes), mirroring the existing `gcx kg prom-rules` family.

## Background: endpoints + wire shapes (verified live 2026-07-28)

Verified via raw passthrough against a real Asserts stack (`assertsdemo01`,
grafana-dev.net — stack-level kg works directly, no IAP). Base path is the same
plugin-resource prefix as the rest of kg:
`/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/config`.

| gcx verb (this branch) | Endpoint | Response shape |
|---|---|---|
| `get [-o yaml\|json]` | `GET .../threshold-rules` | **`PrometheusRulesDto`** = existing `Rule` type. Honors `Accept: application/x-yaml`. |
| `list --category request\|resource` | `GET .../threshold-rules/{request\|resource}` | **`ThresholdRulesDto`** (new type, below). |

Deferred to the follow-up branch (writes):

| gcx verb (follow-up) | Endpoint | Body |
|---|---|---|
| `set -f` | `POST .../threshold-rules/` (x-yaml) | whole-config replace — **destructive**, confirm + `--force` |
| `upsert` | `POST .../threshold-rule` | `{record, expr, labels}` single create-or-update |
| `delete` | `POST .../threshold-rule/delete` | `{record, expr, labels}` single delete (POST-with-body, full-triple identity) |

### `get` shape — reuse `Rule` (`PrometheusRulesDto`)

```jsonc
// GET .../threshold-rules
{ "name": "custom_thresholds",
  "groups": [ { "name": "custom_thresholds",
    "rules": [ { "record": "asserts:latency:average:threshold",
                 "expr": "0.1",
                 "labels": { "job": "integrations/db-o11y", "asserts_request_type": "statements", ... } } ] } ] }
```

Identical to `gcx kg prom-rules` — decode into the existing `Rule` (`types.go`),
wrap via `RuleToResource`, encode through the codec. Default format `yaml`.

### `list --category` shape — new `ThresholdRulesDto`

```jsonc
// GET .../threshold-rules/request  (or /resource)
{ "customThresholds": [ { "active": false, "record": "...", "expr": "0.1", "labels": {...} } ],
  "globalThresholds": [ { "active": false, "record": "...", "expr": "2" } ] }   // labels often omitted (global)
```

New Go types in `internal/providers/kg/types.go`:

```go
// ThresholdRulesDto is the structured per-category threshold view (v1). It is a
// different wire type from the whole-config PrometheusRulesDto/Rule.
type ThresholdRulesDto struct {
    CustomThresholds []Threshold `json:"customThresholds"`
    GlobalThresholds []Threshold `json:"globalThresholds"`
}

type Threshold struct {
    Active bool              `json:"active"`
    Record string            `json:"record"`
    Expr   string            `json:"expr"`
    Labels map[string]string `json:"labels,omitempty"`
}
```

Only `request` and `resource` categories exist in v1 (no `health`).

## Design

### Command tree

```
gcx kg thresholds
  get                      # whole config; default -o yaml
  list --category request|resource   # structured per-category view; -o json|yaml + table
```

Mounted by `newThresholdsCommand(loader)` added to `provider.go` alongside
`newRulesCommand`/`newModelRulesCommand`. Follows the standard opts + `setup(flags)`
+ `Validate()` + constructor pattern; every RunE does `opts.IO.Validate()` →
`loader.LoadGrafanaConfig` → `NewClient(cfg)`.

### Client methods (`client.go`)

Add path constants next to the prom-rules cluster:

```go
thresholdRulesPath        = pluginResourcePath + "/asserts/api-server/v1/config/threshold-rules"
thresholdRulesByCategory  = thresholdRulesPath + "/%s"   // request|resource
// (write paths added in the follow-up)
```

- `GetThresholds(ctx) (*Rule, error)` — GET `thresholdRulesPath`, decode JSON into
  `Rule` (reuse). The codec renders yaml/json; no need to request x-yaml from the
  server (gcx is format-agnostic: fetch data, codec controls display).
- `GetThresholdsByCategory(ctx, category string) (*ThresholdRulesDto, error)` —
  GET `fmt.Sprintf(thresholdRulesByCategory, category)`, decode `ThresholdRulesDto`.

Both use the existing `getJSON` helper. No new HTTP plumbing needed for reads (the
`Accept: x-yaml` variant is only relevant if we dump the server's YAML verbatim;
we don't — we render client-side, consistent with the rest of gcx).

### Output

- **`get`**: reuse the prom-rules path — `RuleToResource(rule, ns)` → unstructured →
  encode. Register the existing `RuleTableCodec`/`RuleWideTableCodec` for table
  views; default format `yaml`.
- **`list`**: value is the `ThresholdRulesDto` (nested). json/yaml codecs marshal it
  faithfully (preserving custom vs global). A custom table codec flattens both arrays
  into rows with a `SCOPE` column (`custom`/`global`) + `RECORD`, `EXPR`, `ACTIVE`,
  and (wide) `LABELS`. Default format `text` (table).

### Registration / conformance

- `internal/agent/command_annotations.go` — add `gcx kg thresholds`, `... get`,
  `... list` entries (`{Cost: "small"}`), mirroring the prom-rules block.
- `cmd/gcx/root/testdata/output_classes.json` — add the two new leaf commands in
  alphabetical order (class `finite`).
- Regenerate reference docs: `GCX_AGENT_MODE=false mise run reference`.

## Testing

Table-driven unit tests in `internal/providers/kg` (httptest-backed client, per the
existing kg test pattern):

- `get` decode: whole-config JSON → `Rule`; table + yaml render.
- `list` decode: `ThresholdRulesDto` for both categories, including the
  global-thresholds-without-labels case; flattened table render; json faithful to
  custom/global split.
- `--category` validation: reject anything other than `request`/`resource`.

## Out of scope (this branch)

- Writes (`set`/`upsert`/`delete`) — follow-up branch; `set` is destructive
  (confirm + `--force`), `delete` is POST-with-body keyed on `{record, expr, labels}`.
- `--dry-run` — no validate endpoint for thresholds, so any future dry-run is
  diff-only. Not needed for read-only.
- v2 (`/v2/config/threshold`) — unadopted by the UI; revisit only if the product migrates.
