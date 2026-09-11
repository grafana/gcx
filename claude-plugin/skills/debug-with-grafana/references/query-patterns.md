# Metrics, logs, and dashboard query patterns

Use only the patterns that answer the next question. Commands below assume a
confirmed context, datasource UIDs, and fixed UTC `FROM`/`TO` values. `api`,
metric names, and labels are examples: substitute the schema you actually found.

## Bounded discovery

Reuse supplied rule/panel queries and configured datasource references. When
the metric and selector are known, run the evidence query directly. Otherwise
choose only the discovery read that resolves the missing name, type, or label;
the commands below are alternatives, not a mandatory sequence.

Scope server-side before filtering names; `--contains` and `--limit` alone do
not bound backend discovery work.

```bash
gcx metrics list-names -d "$PROM_UID" --match '{job="api"}' --contains request --limit 20 -o agents
gcx metrics metadata -d "$PROM_UID" --metric http_requests_total -o agents
gcx metrics labels -d "$PROM_UID" --metric http_requests_total --match '{job="api"}' -o agents
gcx metrics labels -d "$PROM_UID" --metric http_requests_total --match '{job="api"}' --label status -o agents
# Inspect actual label combinations in the investigation interval when needed.
gcx metrics series -d "$PROM_UID" 'http_requests_total{job="api"}' \
  --from "$FROM" --to "$TO" -o agents
```

Repeated `--match` selectors combine as a **union**, not intersection; put
conditions in one selector for AND. Metadata/label discovery is not proof of
data in the incident window. Series discovery can be large; specify a metric,
relevant scope, and time bounds. Do not remove tenant/environment constraints
to work around an empty result without explaining the changed scope.

## Reuse dashboard and alert evidence

Inspect a supplied dashboard immediately if it provides the relevant query.
If discovery is necessary, search narrowly rather than pulling all dashboards:

```bash
gcx dashboards search <service-or-keyword> --limit 10 -o agents
gcx dashboards get <dashboard-uid> -o agents
```

Check the returned `apiVersion` and `spec` before extracting fields:

| Dashboard schema | Inspect |
| --- | --- |
| Legacy | `spec.panels` (including nested row panels), each panel's `targets`/`datasource`; variables in `spec.templating.list` |
| Newer v2 | `spec.elements` map: Panel elements' `spec` contains panel ID/title and data query definitions; variables in `spec.variables` with kind-specific `spec` |

Do not treat absent `spec.panels` as an empty dashboard. Read the element's
actual query kind and datasource references, including nested query specs,
instead of assuming legacy `targets[].expr`. `gcx dashboards get --help` exposes
`--api-version` if you need a specific server-supported version. Do not guess
which versions the server serves.

Resolve template variables, datasource variables, ad hoc filters, and macros
such as `$__rate_interval` before reusing a query outside Grafana. Check query
units and what boundary it measures: a panel title can misdescribe its query.
Record the exact panel ID and variables in evidence links.

Render only when visual state helps answer the question and the renderer is
available. Pin incident times and relevant variables:

```bash
gcx dashboards snapshot <dashboard-uid> --panel <panel-id> \
  --from "$FROM" --to "$TO" --var cluster=<cluster> --var job=api \
  --output-dir ./debug-snapshots
```

This creates local PNGs. Do not render or export inventories as a prerequisite
to investigation. Current alert state is not history; see
[alert-to-trace](alert-to-trace.md) for rule lookup and JSON shape.

## Prometheus: define population and aggregation

Use the actual status/operation labels (`status`, `code`, `handler`, `route`,
etc.). Apply the same environment/tenant scope on numerator and denominator.
Rate counters before aggregating so counter resets are handled per series.
Empty, zero, and non-finite results are different states.

### HTTP error ratio

Aggregate away status and instance on both sides, retaining the intended common
dimensions. Here one result describes all requests for the selected job:

```bash
gcx metrics query -d "$PROM_UID" \
  'sum by (job) (rate(http_requests_total{job="api",status=~"5.."}[5m])) / sum by (job) (rate(http_requests_total{job="api"}[5m]))' \
  --from "$FROM" --to "$TO" --step 1m -o agents
```

Unaggregated division matches each error series to itself, often yielding 1.
For per-route ratios, retain the route label on both sides. Missing numerator
series are not automatically zero errors: establish coverage/export behavior
before supplying zeros. A zero denominator cannot yield a meaningful ratio.
Inspect total request rate too, so a ratio increase is not mistaken for a
volume increase.

### Classic histogram P95

Combine buckets across instances **before** calculating the quantile. Preserve
`le` plus the intended grouping (here `job`):

```bash
gcx metrics query -d "$PROM_UID" \
  'histogram_quantile(0.95, sum by (job, le) (rate(http_request_duration_seconds_bucket{job="api"}[5m])))' \
  --from "$FROM" --to "$TO" --step 1m -o agents
```

For per-endpoint latency retain the endpoint label alongside `job, le`. Do not
average instance P95s or combine unlike populations/bucket layouts without
checking compatibility. A percentile is not an additive latency share.

### Native histogram P95

A native histogram is the base metric, not a `_bucket` family. Do not invent an
`le` grouping for it:

```bash
gcx metrics query -d "$PROM_UID" \
  'histogram_quantile(0.95, sum by (job) (rate(http_request_duration_seconds{job="api"}[5m])))' \
  --from "$FROM" --to "$TO" --step 1m -o agents
```

Select the example matching actual metadata and samples. Check response
warnings for incompatible histograms or mixed sample types. If only sum/count
series exist, a ratio of aggregated rates gives the mean, **not** a P95.

### Scrape status

```bash
gcx metrics query -d "$PROM_UID" 'up{job="api"}' --time "$TO" -o agents
```

- `1`: that scrape succeeded, not proof every application operation is healthy.
- `0`: that scrape failed; inspect target, network, auth, or scrape errors.
- Empty vector: no matching series at that evaluation time. Configuration,
  staleness, retention, or missing telemetry may explain it.

### Absent scrape series

```bash
gcx metrics query -d "$PROM_UID" 'absent(up{job="api"})' \
  --from "$FROM" --to "$TO" --step 1m -o agents
```

`absent(...)` returns a series with value **1** when no input series matches.
It returns no sample when any matching series exists, even one with `up=0`.
Neither absence nor the last observed sample proves when a process crashed.

## Time semantics and coverage

### Window request total

For a total over a window ending at one selected timestamp, use an **instant**
query. This is valid with `increase()` and other over-time functions:

```bash
gcx metrics query -d "$PROM_UID" \
  'sum by (job) (increase(http_requests_total{job="api"}[30m]))' --time "$TO" -o agents
```

`increase()` accounts for observed counter resets and extrapolates to the
window boundaries; it is not an exact event ledger. Check scrape coverage.
Do not sum overlapping sliding-window totals from a range query.

### Window gauge average

For the mean of recorded gauge samples over a window ending at `TO`:

```bash
gcx metrics query -d "$PROM_UID" \
  'avg_over_time(queue_depth{job="api"}[30m])' --time "$TO" -o agents
```

This is per-series and sample-weighted, not a traffic-weighted average or the
current queue depth. Keep instances separate or aggregate only when the metric's
semantics justify it. A queue-depth gauge does not measure enqueue counts.

Use `--from`/`--to` for **trends** and `--time` for an instant evaluation; they
are mutually exclusive. Pin comparison timestamps rather than repeatedly
resolving `now`. RFC3339 and Unix timestamps are supported; relative expressions
such as `now-1h` are useful for initial live triage. Do not chain relative
subtractions. Step values need units (for example `1m` or `300s`).

Choose the rate lookback for the actual scrape interval (enough samples to
estimate a rate), and step for useful resolution and cost. A smaller step
cannot restore data that was not recorded.

Before using a result quantitatively:

- Inspect actual first/last timestamps, spacing, gaps, and units for each series.
- Verify incident and comparison windows have suitable, comparable coverage.
- If requested 1h steps return 6h-spaced points, do not claim hourly resolution
  or blame a particular layer without evidence. Narrow once to verify, inspect
  the source query/recording interval, then report the observed resolution.
- Distinguish current rate, mean rate, window total, and percentile. Do not
  compare a current rate with a weekly average as if they were the same measure.

### Grafana Cloud aggregated metrics

Some series are available only through supported aggregations. A rejected raw
selector or aggregation is not proof of missing telemetry. Reuse the working
recording-rule/dashboard query, inspect its output labels, and preserve its
supported aggregation. Add filters only on retained dimensions. If a tenant or
other label has been aggregated away, you cannot recover that scope by filtering
the result; choose another source or state that the requested attribution is
unavailable. Do not silently broaden the population to make a query succeed.

## Loki: sample details separately from counts

### Learn fields from bounded logs

With a known stream selector, a bounded query provides evidence and reveals
per-entry fields in the same read; do not add a label-discovery pass first:

```bash
gcx logs query -d "$LOKI_UID" '{service_name="api"}' \
  --from "$FROM" --to "$TO" --limit 10 -o agents
```

Only if needed to construct the selector, discover indexed names or values:

```bash
gcx logs labels -d "$LOKI_UID" -o agents
gcx logs labels -d "$LOKI_UID" -l service_name -o agents
```

| Kind | JSON location | LogQL use |
| --- | --- | --- |
| Indexed labels | `data.result[].stream` | Inside `{service_name="api"}` |
| Structured metadata | Entry `structuredMetadata` | After a pipe, e.g. `\| detected_level="error"` |
| Parsed fields | Entry `parsed` after parsing | After `\| json`/`\| logfmt`, e.g. `\| status="500"` |

Loki's auto-added `detected_level` is structured metadata, not an indexed label.
`logs labels` enumerates indexed labels, not every key seen in a line. A stream
selector needs a matcher that cannot match the empty string; avoid `{}` and
broad catch-alls. `logs series -M` can discover indexed combinations but has no
time flags in this build; use scoped line queries to verify incident coverage.

### Targeted error details

```bash
gcx logs query -d "$LOKI_UID" \
  '{service_name="api"} | json | trace_id="<trace-id>" | __error__=""' \
  --from "$FROM" --to "$TO" --limit 20 -o agents
```

Choose actual fields and parsers. If `trace_id` is structured metadata, filter
it after a pipe without parsing the body. gcx defaults to **50 log lines**;
explicit limits bound samples, not frequency. Omitting the CLI cap does not
establish complete backend coverage. The earliest returned line is only the
earliest **observed in that sample**, not necessarily the first occurrence.

### Log-derived trends

Use `logs metrics`, not the log-line `logs query` output path, for aggregate
LogQL. Aggregate to the dimensions you need to reduce returned cardinality:

```bash
gcx logs metrics -d "$LOKI_UID" \
  'sum(rate({service_name="api"} | json | level="error" | __error__="" [5m]))' \
  --from "$FROM" --to "$TO" --step 1m -o agents

# Rolling count of observed matching entries, not independent 5m buckets.
gcx logs metrics -d "$LOKI_UID" \
  'sum(count_over_time({service_name="api"} | json | status >= 500 | __error__="" [5m]))' \
  --from "$FROM" --to "$TO" --step 1m -o agents
```

Place `__error__=""` after stages that can create errors, including numeric
conversions. Metric queries reject pipeline errors; filtering them excludes
those entries, so disclose parse failures when they affect the conclusion.
Aggregation reduces output, but does not guarantee a cheap scan or avoidance of
all intermediate series limits. Narrow indexed scope/time before broadening.

`logs metrics` without time flags evaluates at now; it has no Prometheus-style
`--time`. For historical counts use explicit ranges and the correct rolling
window semantics; do not invent an instant-time flag or sum overlapping windows.
To investigate first occurrence, use aggregate trends to narrow an onset window,
then inspect bounded logs there; qualify the onset by available retention and
coverage.

### Define what you count

An HTTP request can log at gateways, callers, backends, and on each retry. Do
not sum those hops as unique requests. Choose one authoritative request boundary
for totals/shares and verify that it logs one event per unit being counted.
Gateway metrics may be the correct boundary for user-visible outcomes. Backend
attempts may be correct for dependency load; name them as attempts.

A trace ID need not equal one application request (batches and async work exist).
Do not recommend high-cardinality trace-ID grouping as a universal deduplication
fix. For endpoint ownership, distinguish error **counts/shares** from per-endpoint
error **ratios**; for retries, distinguish attempts from original requests.

## Output and evidence links

Start with normal agent output; examples pin it with `-o agents`. Inspect the
inline result directly. An automatic spill points to saved data, not an empty
result; inspect that data without repeating the remote read. A harness may also
truncate output before GCX spills: recover its saved result when available, or
narrow the query. Do not treat a truncated preview as complete evidence.

Use `--json` or `--jq` upfront only when the needed fields and transformation are
known. Otherwise let the bounded evidence read reveal them, rather than adding
a throwaway schema query. Both are native GCX features; no external jq dependency
or installation preflight is needed. Use one flag, not both, and omit `-o agents`
with either. They transform command output, not saved spill files; use available
local file tools for those. Projection reduces model input, not backend work.

### Optional native transformations

These are examples, not a required processing stage. Keep stdout and stderr
separate; never pipe `2>&1` into a JSON parser. The filters below modify only
successful results of the expected kind, preserving the surrounding response,
including notices and warnings. Retain stderr diagnostics too.

Rank the five routes with the largest estimated 5xx counts. PromQL bounds the
result; `--jq` orders the returned vector numerically without a separate script:

```bash
gcx metrics query -d "$PROM_UID" \
  'topk(5, sum by (route) (increase(http_requests_total{job="api",status=~"5.."}[30m])))' \
  --time "$TO" --jq '
    if .status == "success" and .data.resultType == "vector"
      and all(.data.result[]; .value[1] | try (tonumber | isfinite) catch false)
    then .data.result |= (sort_by(.value[1] | tonumber) | reverse)
    else . end'
```

Metric labels and evaluation timestamps remain attached to values; non-finite
or unparseable values leave the response unchanged. This is a top-five ranking,
not the total error count, an error ratio, or proof that other routes are
unaffected. Use a matching denominator if the question needs shares; do not
interpret empty or non-finite results as zero errors.

For verbose parsed logs, return timestamps, parsed fields, and structured
metadata without duplicating the raw line body. Use this projection only after
confirming the needed evidence is in those fields:

```bash
gcx logs query -d "$LOKI_UID" \
  '{service_name="api"} | json | level="error" | __error__=""' \
  --from "$FROM" --to "$TO" --limit 5 --jq '
    if .status == "success" and .data.resultType == "streams" then
      .data.result |= map(.values |= map({timestamp, parsed, structuredMetadata}))
    else . end'
```

Stream labels and response notices are retained. Five returned lines may hit the
sample cap; this sample cannot establish frequency or absence elsewhere. Keep
query scope, time bounds, and the limit with the evidence. If raw line details
are needed, return the bounded response with `-o agents` and no projection instead.

### Evidence links

Add `--share-link` to an already-needed metrics/logs range query or a bounded
trace search/get with fixed `--from`/`--to` timestamps to capture a reproducible
Explore link.

**Exception: historical Prometheus instant queries.** With `--time "$TO"`,
the query uses the selected timestamp, but the generated Explore link currently
ignores `--time` and opens `now-1m` → `now`. Until link generation preserves
`--time`, do not use that link as reproducible incident evidence; retain the
exact expression, datasource UID, and evaluation timestamp instead.

Keep supplied dashboard links with exact panel IDs and variable values. Prefer
these to speculative hand-built URLs. `-o graph` is useful for human metric trends,
not a replacement for inspecting actual timestamps and values.
