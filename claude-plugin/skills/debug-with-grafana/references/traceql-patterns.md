# TraceQL patterns

Use observed service/operation names and attribute types. The main skill and
[trace comparison](trace-comparison.md) define when and why to query Tempo.
These are syntax examples, not a required discovery sequence.

## Command surface

| Command | Purpose |
| --- | --- |
| `gcx traces query [TRACEQL]` | Bounded trace search; `search` is an alias |
| `gcx traces get TRACE_ID --llm -o json` | Fetch an execution for agent analysis |
| `gcx traces labels` | Discover attribute names |
| `gcx traces tags -l TAG --llm -o json` | Compact attribute values; `tags` aliases `labels` |
| `gcx traces baseline TRACE_ID` | Experimental same-operation baseline candidates |
| `gcx traces diff BASELINE_ID SEED_ID` | Experimental server-side execution comparison; Grafana Cloud-only |
| `gcx traces metrics [TRACEQL]` | Aggregate metrics over observed tracing data |

All accept `-d <tempo-uid>`. A trace ID is positional, not `--trace-id`.
Search has no `--service` or `--tag` flag; put those conditions in TraceQL.
Check `gcx help-tree traces -o text` for the running build's capabilities.

## Scoped discovery

Only discover attributes that could change the next query. Reuse the known
service to narrow names/values:

```bash
gcx traces labels -d "$TEMPO_UID" --scope span \
  --query '{ resource.service.name = "<service>" }' -o json
gcx traces tags -d "$TEMPO_UID" -l span.http.route \
  --query '{ resource.service.name = "<service>" }' --llm -o json
```

Names/values discovery has no time flags in this build. Verify incident
coverage with bounded search/get rather than inferring it from tag presence.
`--llm` for values requires `-l`; unsupported LLM encodings may return standard
JSON, so inspect the actual response.

## Attribute scopes and intrinsics

| Expression | Meaning |
| --- | --- |
| `resource.service.name`, `resource.k8s.cluster.name` | Resource attributes |
| `span.http.route`, `span.http.response.status_code` | Span attributes; names depend on instrumentation |
| `name` / `span:name` | Span operation name |
| `duration` / `span:duration` | Span duration |
| `status` / `span:status` | `error`, `ok`, or `unset` (unquoted enums) |
| `kind` / `span:kind` | `server`, `client`, `producer`, `consumer`, `internal` |
| `trace:rootService`, `trace:rootName` | Root service and operation |
| `trace:duration` | End-to-end trace duration |

Use explicitly scoped custom attributes. Bare dotted `service.name` or
`http.status_code` is not the appropriate scoped syntax. Search response fields
`rootServiceName`/`rootTraceName` are not TraceQL intrinsics.

## Match the symptom and cohort

```bash
# Error spans on the affected service/operation.
gcx traces query -d "$TEMPO_UID" \
  '{ resource.service.name = "<service>" && name = "<operation>" && status = error }' \
  --from "$FROM" --to "$TO" --limit 10 -o json

# Slow server spans, with the threshold derived from the actual symptom.
gcx traces query -d "$TEMPO_UID" \
  '{ resource.service.name = "<service>" && name = "<operation>" && kind = server && duration > <threshold> }' \
  --from "$FROM" --to "$TO" --limit 10 -o json

# End-to-end latency is a different population/measurement.
gcx traces query -d "$TEMPO_UID" \
  '{ trace:rootService = "<service>" && trace:rootName = "<operation>" && trace:duration > <threshold> }' \
  --from "$FROM" --to "$TO" --limit 10 -o json
```

Conditions inside one `{ ... }` must hold on the same span. Spanset conjunction
`{ ... } && { ... }` can match different spans of a trace. Use the latter only
when trace-level coexistence is intended, not as proof of parent/child causality.
HTTP response-status attributes and span error status are distinct signals.

Search is capped (default 20 in this build), and order is not similarity or
severity ranking. Use scoped, bounded queries to retrieve examples, not to
estimate incident shares by counting returned rows.

## Inspect and compare

```bash
gcx traces get -d "$TEMPO_UID" "$SEED" --llm -o json
```

Prefer the backend's compact trace encoding. Do not fetch raw OTLP and write a
custom compactor for agent analysis. Omit `--llm` only when raw schema/export
work is requested; the backend may also fall back to standard JSON itself.
Inspect partiality and missing parents before making structural claims.
Continue with [trace comparison](trace-comparison.md) for candidates, diff
orientation, topology bias, and capability fallbacks.

## TraceQL metrics: observed population, not all traffic

When supported, aggregate observed server spans for a verified operation:

```bash
gcx traces metrics -d "$TEMPO_UID" \
  '{ resource.service.name = "<service>" && name = "<operation>" && kind = server } | rate()' \
  --from "$FROM" --to "$TO" --step 1m -o json
```

This describes matching spans, not automatically unique requests. Sampling,
retention, instrumentation coverage, and backend query behavior affect the
result. Compare like cohorts and validate coverage before making prevalence
claims; never use stored-trace metrics to infer how much pre-sampling input was
discarded. Prefer independent request/ingestion counters for that denominator.

Time flags normally select a range query; `--instant` requests an instant query
over the supplied interval when supported. Do not copy Prometheus `--time` or
PromQL `rate(metric[5m])` syntax into TraceQL. If metrics are unsupported, keep
using usable trace exemplars and state the population-measurement limitation.
