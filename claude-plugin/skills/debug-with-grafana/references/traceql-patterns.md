# TraceQL patterns

Use observed service/operation names and attribute types. The main skill owns
orchestration; [alert-to-trace](alert-to-trace.md) handles seed selection and
[trace comparison](trace-comparison.md) handles controls. When no usable ID exists,
scoped discovery helps construct a selective query. Reuse what is already known.

## Command surface

| Command | Purpose |
| --- | --- |
| `gcx traces query [TRACEQL]` | Bounded trace search; `search` is an alias |
| `gcx traces get TRACE_ID --llm -o agents` | Fetch an execution for agent analysis |
| `gcx traces labels` | Discover attribute names |
| `gcx traces tags -l TAG --llm -o agents` | Compact attribute values; `tags` aliases `labels` |
| `gcx traces baseline TRACE_ID` | Experimental same-operation baseline candidates |
| `gcx traces diff BASELINE_ID SEED_ID` | Experimental server-side execution comparison; Grafana Cloud-only |
| `gcx traces metrics [TRACEQL]` | Aggregate metrics over observed tracing data |

All accept `-d <tempo-uid>`. A trace ID is positional, not `--trace-id`.
Search has no `--service` or `--tag` flag; put those conditions in TraceQL.
Check `gcx help-tree traces -o text` only if command support is unknown.

## Scoped discovery

Tags and tag values are useful preparation when no usable trace ID is available,
including when a log-derived ID cannot be retrieved. Do not wait for a guessed
TraceQL query to fail. Discover only the names or values needed to select the
affected cohort; a known usable ID skips this path.

If identity attribute names are unknown, start with resource-scoped names:

```bash
gcx traces tags -d "$TEMPO_UID" --scope resource -o agents
```

Once a cohort selector is verified, use it to narrow subsequent discovery.
For example, with a known service, inspect span attributes and then values of
an observed route attribute; replace these illustrative names as needed:

```bash
gcx traces tags -d "$TEMPO_UID" --scope span \
  --query '{ resource.service.name = "<service>" }' -o agents
gcx traces tags -d "$TEMPO_UID" -l span.http.route \
  --query '{ resource.service.name = "<service>" }' --llm -o agents
```

If a needed value such as the trace service name is unknown, query that observed
attribute with `-l` before using it in a selector. Reuse known names and values;
do not enumerate all values for every returned tag.

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
  --from "$FROM" --to "$TO" --limit 10 -o agents

# Slow server spans, with the threshold derived from the actual symptom.
gcx traces query -d "$TEMPO_UID" \
  '{ resource.service.name = "<service>" && name = "<operation>" && kind = server && duration > <threshold> }' \
  --from "$FROM" --to "$TO" --limit 10 -o agents

# End-to-end latency is a different population/measurement.
gcx traces query -d "$TEMPO_UID" \
  '{ trace:rootService = "<service>" && trace:rootName = "<operation>" && trace:duration > <threshold> }' \
  --from "$FROM" --to "$TO" --limit 10 -o agents
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
gcx traces get -d "$TEMPO_UID" "$SEED" --llm -o agents
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
  --from "$FROM" --to "$TO" --step 1m -o agents
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

### Metrics arithmetic

TraceQL metrics supports `+`, `-`, `*`, and `/` between metrics queries and
numeric scalars; ratios do not require separate queries and client-side math.
Wrap **each metrics subquery** in parentheses. These examples follow Tempo's
[arithmetic expressions reference](https://grafana.com/docs/tempo/latest/metrics-from-traces/metrics-queries/functions/#arithmetic-expressions).
Support depends on the deployed Tempo backend, not the gcx version. If the
backend rejects arithmetic, retain usable individual metrics queries and state
the limitation rather than declaring the syntax invalid everywhere.

Use verified attributes and values in place of the placeholders. Apply the same
environment, cluster, service, and operation constraints to numerator and
denominator, except for the condition whose share is being measured. The
examples below measure stored server spans, not automatically unique requests
or unsampled traffic.

```bash
# Error percentage for one service: multiply the ratio by 100.
gcx traces metrics -d "$TEMPO_UID" \
  '100 * ({ resource.service.name = "<service>" && kind = server && status = error } | rate()) / ({ resource.service.name = "<service>" && kind = server } | rate())' \
  --from "$FROM" --to "$TO" --step 1m -o agents

# Per-service error percentage; match both sides by the same labels.
# At each time step, keep up to five highest percentages, then retain points > 5.
# The selected services can change across steps; the range can contain > 5 series.
gcx traces metrics -d "$TEMPO_UID" \
  '100 * ({ kind = server && status = error } | rate() by (resource.service.name)) / ({ kind = server } | rate() by (resource.service.name)) | topk(5) > 5' \
  --from "$FROM" --to "$TO" --step 1m -o agents

# Each service's share of observed server-span throughput.
# The unlabeled denominator is broadcast to every labeled numerator series.
gcx traces metrics -d "$TEMPO_UID" \
  '100 * ({ kind = server } | rate() by (resource.service.name)) / ({ kind = server } | rate())' \
  --from "$FROM" --to "$TO" --step 1m -o agents

# Convert spans/second to spans/minute (not a count over the whole window).
gcx traces metrics -d "$TEMPO_UID" \
  '({ resource.service.name = "<service>" && kind = server } | rate()) * 60' \
  --from "$FROM" --to "$TO" --step 1m -o agents

# Add rates for two distinct services; this counts spans across both services,
# not deduplicated end-to-end requests.
gcx traces metrics -d "$TEMPO_UID" \
  '({ resource.service.name = "<service-a>" && kind = server } | rate()) + ({ resource.service.name = "<service-b>" && kind = server } | rate())' \
  --from "$FROM" --to "$TO" --step 1m -o agents

# Signed distance from an illustrative 100-server-spans/minute threshold.
# Replace 100 with the threshold relevant to the investigation.
gcx traces metrics -d "$TEMPO_UID" \
  '({ resource.service.name = "<service>" && kind = server } | rate()) * 60 - 100' \
  --from "$FROM" --to "$TO" --step 1m -o agents

# Mean response size in MiB; first verify this numeric byte attribute exists.
gcx traces metrics -d "$TEMPO_UID" \
  '({ resource.service.name = "<service>" && kind = server } | avg_over_time(span.http.response.body.size)) / (1024 * 1024)' \
  --from "$FROM" --to "$TO" --step 1m -o agents
```

For count ratios, use `count_over_time()` on both sides instead of `rate()`.
Keep units compatible: adding a per-second rate to a bucket count is not a
meaningful total, even if the expression parses.

Rules that differ from common PromQL assumptions:

- Same `by(...)` attributes on both sides match series by label. An unlabeled
  side is applied to every labeled series on the other side; do not add PromQL
  `on(...)`, `ignoring(...)`, or `group_left` syntax to these examples.
- Trailing `topk`, `bottomk`, and comparisons apply to the **whole arithmetic
  expression**, not its final operand. In the example above, Tempo computes
  `100 * numerator / denominator`, then applies `topk(5)`, then `> 5`.
  Outer parentheses around the arithmetic expression are optional; each metrics
  subquery still requires its own parentheses. A percentage query uses `> 5`
  for five percent; an unscaled ratio uses `> 0.05`.
- `topk(k)` and `bottomk(k)` select independently at each time step, not once
  across the range. They retain up to `k` values per step; changing membership
  means a range query can return more than `k` distinct series. Non-selected
  points become `NaN`, not zero.
- Division by zero and a bucket with no matching spans produce `NaN`.
  Arithmetic propagates it: neither `+ 0` nor `* 0` repairs missing data.
  A missing error bucket is not evidence of a zero-percent error rate.
- Scalar-only integer arithmetic truncates: `1 / 2` is `0`; use `1.0 / 2.0`
  for `0.5`. Scalars can appear on either side; use `(-100)` for a negative
  operand when multiplying.
- Duration literals such as `10s` are not valid scalar operands, and
  `compare()` cannot be used in arithmetic expressions.
- Bare `{} | rate() + {} | rate()` is invalid; use
  `({} | rate()) + ({} | rate())`.
