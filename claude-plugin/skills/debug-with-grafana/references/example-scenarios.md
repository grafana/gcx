# Investigation scenarios

These examples illustrate decisions, not a universal command sequence. Apply
the main skill's context and evidence rules. Resolve datasource UIDs and actual
schemas before adapting queries. Fixed UTC `FROM`/`TO` values describe the
incident; comparison windows need their own justification. No example below
asserts live telemetry values.

## 1. Alert-led request regression, all signals available

**Input:** A pasted Alertmanager notification reports server-request latency on
one operation after a rollout. It includes per-alert service/cluster labels,
a firing timestamp, and a dashboard link, but no trace ID or expression.

**Question:** Where did execution change, and does the change explain the
regression?

1. Keep per-alert labels. Retrieve the expression from its actual rule source
   if needed; no IRM prerequisite. Expand before firing time for rule lookback
   and pending duration. Inspect the linked panel's query, schema, and variables.
2. Run the existing latency query for onset, scope, and an old-version control.
   Compare request mix and traffic volume before attributing a rollout effect.
3. Map the affected workload to the actual trace service/operation. Search for
   server spans matching the alert's threshold and inspect a representative seed
   using [alert-to-trace](alert-to-trace.md).
4. Retrieve and qualify controls; prefer the justified pre-regression interval:

   ```bash
   gcx traces baseline -d "$TEMPO_UID" "$SEED" \
     --from "$GOOD_START" --to "$GOOD_END" --limit 20 -o json
   gcx traces diff -d "$TEMPO_UID" "$BASELINE" "$SEED" -o json
   ```

5. Let the delta choose the next query. If it shows more dependency calls,
   inspect attempt/retry evidence and relevant configuration changes; do not
   assume repeated calls are retries. If a dependency span alone grew, inspect
   its latency and contention instead. Target any log query to that operation,
   trace ID, and interval.
6. Repeat a qualified comparison when needed to distinguish a systematic change
   from request variation. Check unaffected/version controls and population
   evidence for blast radius.

**Stop/report:** Name the changed execution path and evidence supporting or
contradicting rollout causation. Include seed/control IDs, comparable dimensions,
exact panel/Explore links, observed differences, and uncertainty. Localization
without corroborating change evidence is not a proven deployment root cause.

## 2. Supplied trace ID

**Input:** "Why is this request slow? Here is the trace ID and datasource."

```bash
gcx traces get -d "$TEMPO_UID" "$SEED" --llm -o json
```

Inspect immediately after confirming the target. Do not run generic metrics,
logs, or datasource inventories first. If execution comparison could explain
the delay, enter [trace comparison](trace-comparison.md). A matched successful
request near the seed may be a better control than a different workload yesterday;
non-error status alone is not sufficient for a latency control.

If the trace already answers the requested distinction, report it and stop.
Use metrics only if the user asks about prevalence or the conclusion needs them.
Without population evidence, describe this request, not all requests.

## 3. Ingestion declined: sampling or less input?

**Input:** "Did ingestion drop because Adaptive Traces discarded more spans,
or because less data reached Grafana?" Tenant and time window are supplied.

**Question:** Compare pre-sampling input with post-sampling ingestion for the
same tenant, interval, units, and pipeline boundary. This is not a request-path
question; stored trace pairs cannot measure discarded input.

1. Inspect the relevant tenant dashboard's actual queries. Do not infer a
   counter's boundary from a title such as "Head Sampled Bytes Received".
2. Where available and authorized, inspect these metric families and reuse the
   supported, tenant-scoped panel aggregations:
   - `adaptive_traces_sampler_gateway_preprocessing_spans_received_total`
   - `adaptive_traces_sampler_gateway_preprocessing_bytes_received_total`
   - `tempo_distributor_spans_received_total`
   - `tempo_distributor_bytes_received_total`
3. Compare input and ingestion trends and their relative gap, accounting for
   pipeline delay, drops/retries, retained labels, and returned resolution.
   Do not substitute policy metrics for a different tenant. Match sampling
   policy scope to the actual subject.
4. Compare like measurements: current rates with current rates, window totals
   with window totals. Do not label sparse 6h points as hourly because `--step`
   requested 1h. Use the actual query and timestamps as evidence.

**Interpretation:** If input and ingestion fall together without an increasing
relative loss attributable to sampling, that supports reduced incoming telemetry
rather than increased sampling loss. Confirm the counters measure the same
pipeline; a coincident decline alone is not sufficient.

**Stop/report:** Explain what the counters distinguish, cite the exact dashboard
panels/queries, and stop. Reduced input to Grafana does not distinguish reduced
application traffic from customer-side filtering/sampling or exporter loss.
Transport errors can be follow-up, not a reason to hijack the original question.
No trace diff is needed to answer this distinction.

## 4. Endpoint errors: count, ratio, or share?

**Input:** "Which endpoint owns most 5xx in this incident window?"

Use one authoritative request boundary, not the sum of gateway, backend, and
retry logs. Discover the actual route/status labels. Calculate window error
counts by route; divide each by all errors for **share of errors**. Per-route
errors divided by that route's requests is a different question: **error ratio**.
Use instant `increase()` at the window end for counter totals, not a sum of
overlapping range-query windows. See [query patterns](query-patterns.md).

**Stop/report:** Rank observed contributions with denominator, interval, and
coverage. A rare route can have a high error ratio but contribute few errors.
Do not perform trace RCA unless the user also asks why or that explanation is
necessary to establish attribution.

## 5. Missing signals and comparison failures

| Available evidence / obstacle | Expected behavior | Stop or disclose |
| --- | --- | --- |
| Metrics only | Use relevant counters/gauges/histograms and change/resource evidence | Request-level mechanics may remain unlocalized; do not set up Tempo automatically |
| Logs only | Target indexed scope and time; use `logs metrics` for aggregate LogQL, line queries for details | Limited samples do not establish frequency or first occurrence; verify logging coverage |
| Traces only | Search or fetch seeds, qualify baselines, diff; use supported TraceQL metrics when appropriate | Sampling/coverage constrain population claims; no Prometheus prerequisite |
| Metrics/logs, no Tempo | Answer from the usable signals and test alternatives | State only the request-level uncertainty that matters; no tracing setup gate |
| Baseline absent in the installed CLI | Scoped same-operation TraceQL search in a justified window | Qualify results manually; do not invent a baseline command variant |
| Diff unavailable on the backend | Fetch qualified traces with `--llm` and compare execution evidence manually | State that server-side diff was unavailable |
| New dependency causes automatic retrieval to reject old controls | Search same operation without pinning the seed's downstream topology | Do not conclude no healthy controls exist; a stricter `--filter` cannot relax generated constraints |
| Seed partial or candidates invalid | Inspect completeness/context, try a better seed or refined cohort | No fabricated RCA if a valid comparison remains unavailable |
| Candidates overlap the incident | Seek a known-good or justified contemporaneous control, verify latency/outcomes | Non-error roots and zero reported error counts do not guarantee health |
| No useful telemetry/access | Reuse supplied evidence, identify the smallest additional requirement | Stop; distinguish inaccessible from no matching data and from not checked |
| User's distinction already answered | Report decisive evidence and limitations | Do not chase a separate deeper root cause |

## Evaluating the workflow

Replay these scenarios with controlled telemetry fixtures and the same model,
configuration, and access when comparing skill versions. Include an unrelated
expired context and an Alertmanager input without IRM in the fixtures. Evaluate:

- Correct answer and calibrated uncertainty, not just command execution.
- Time/queries to decisive evidence and unnecessary discovery/scans.
- Whether baseline/diff correctly localize request changes when useful.
- Whether topology bias, partiality, unavailable features, and missing signals
  trigger the documented fallbacks.
- Links that identify the exact query/window, panel, or trace behind a claim.
- Stopping after the requested question is answered.

Command-tree validation checks command/flag spelling, not query semantics or
agent behavior. Synthetic query tests are separate from model replays; neither
proves the experimental backend works on an untested stack. Different models
or unequal access do not establish a causal skill speedup.
