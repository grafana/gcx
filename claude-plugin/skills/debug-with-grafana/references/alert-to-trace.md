# From an alert or symptom to a representative trace

Use this reference when request execution remains unexplained and you need a
representative trace. A supplied ID is a direct lookup opportunity, not a promise
that the trace was retained. Confirm its target, fetch it, and inspect it if usable.

## Normalize the input without requiring IRM

Accept Grafana Alerting, Alertmanager, another alert provider, pasted text,
dashboards, or a symptom. Capture:

- Alert name, symptom, expected behavior, and whether it is still occurring.
- Each alert's labels: service/workload, operation, tenant, environment, region.
  Grouped `commonLabels` can omit dimensions that differ between alerts.
- `startsAt`/`endsAt`, pending duration, expression/lookback if available.
- Rule source, runbook/dashboard links, trace IDs/exemplars, and known changes.

An Alertmanager notification need not contain the rule expression. Retrieve it
from the named rule source only if needed to understand the condition. Do not
assume Grafana owns a datasource-managed rule. Use `investigate-alert` for
rule semantics; IRM is only an optional context source.

For a Grafana-managed rule with a known UID:

```bash
gcx alert rules get <rule-uid> -o agents
```

If the rule UID is unknown and current firing state is relevant, narrow by a
known folder/group before inspecting a bounded list:

```bash
gcx alert rules list --folder <folder-uid> --state firing --limit 20 -o agents
```

JSON is an array of groups, each with a `rules` array. `rules[].state` is current
evaluation state; this is not incident history. The current build's structured
list limit applies to groups, not individual rules. No result on a limited
page is not proof that no relevant alert exists. For historical investigations,
use supplied notification/history evidence rather than treating today's state
as the state during the incident.

## Establish the affected cohort

Use an existing alert/recording-rule/panel query to find onset and the dimensions
that distinguish affected requests. Include preceding time for smoothing and
pending duration; alert firing time is not necessarily failure onset.

Translate identity across signals. A scrape `job` can identify a collector or
multiple services. Verify its relationship to trace `resource.service.name`,
namespace/cluster, operation, and tenant rather than copying label names.

Match the measured symptom:

| Alert measures | Trace search should test |
| --- | --- |
| A service's server-request latency | That service/operation's server-span duration |
| End-to-end request latency | Root identity and `trace:duration` |
| HTTP 5xx | The observed HTTP response status attribute on the relevant span |
| Span errors | `status = error` on the relevant span |

HTTP response status and span status are not interchangeable. Attribute names
vary by instrumentation, for example `span.http.response.status_code` versus
`span.http.status_code`. Discover the actual field and its type. Do not assume
an arbitrary 1s threshold corresponds to the alert.

## Reach the seed by the shortest useful route

```text
Known ID from a link, exemplar, or log → fetch → usable? inspect
                                              otherwise ↓
No usable ID → scoped tags/values → bounded cohort search → inspect a retained example
```

Use an existing exemplar ID/link; do not invent a gcx exemplar command. Do not
scan logs merely to obtain an ID when a scoped trace search can find the seed.

### When a known ID is unavailable

If retrieval reports no trace, check the supplied datasource and lookup bounds;
correct a concrete mismatch once. Otherwise record the ID as unavailable and
search the affected cohort, rather than repeatedly fetching that ID or cycling
through more log IDs. Sampling (including Adaptive Traces), retention, and
ingestion delays can explain missing data; do not claim which caused it without
evidence. A failed request is not a missing trace: handle authentication,
permission, or backend errors through [error recovery](error-recovery.md).

### Prepare a cohort search

When no usable ID exists, use [scoped tags and tag values](traceql-patterns.md#scoped-discovery)
to learn the attributes, types, and values needed for a selective TraceQL query.
This is query preparation, not merely error recovery. Reuse known attributes
and scope; do not enumerate every tag or run a discovery tour before fetching
a usable known ID. Tag presence alone does not establish incident coverage.

Search for the affected service, operation, interval, and symptom. A retained
example must independently exhibit that symptom; it is not the missing request
and does not prove the same mechanism occurred there. If no relevant retained
evidence is found, use other signals or report the request-level limitation.

Example searches using the verified schema (replace placeholders):

```bash
# Duration here is server-span duration, not total trace duration.
gcx traces query -d "$TEMPO_UID" \
  '{ resource.service.name = "<service>" && name = "<operation>" && kind = server && duration > <threshold> }' \
  --from "$FROM" --to "$TO" --limit 10 -o agents

# For an HTTP-error symptom, use the actual response-status attribute/type.
gcx traces query -d "$TEMPO_UID" \
  '{ resource.service.name = "<service>" && name = "<operation>" && span.http.response.status_code >= 500 }' \
  --from "$FROM" --to "$TO" --limit 10 -o agents
```

Search returns exemplars, not a ranked or statistically representative sample.
Sampling can bias the retained cohort; use independent metrics/logs with known
coverage for incident frequency and blast radius. A cap of 10 is a starting
bound, not a completeness guarantee. Narrow/refine the cohort when results are
unrelated; do not fetch every trace or automatically select the first result
or slowest outlier.

```bash
gcx traces get -d "$TEMPO_UID" "$SEED" --llm \
  --from "$FROM" --to "$TO" --share-link -o agents
```

Inspect the body for the affected operation, duration/error evidence, relevant
context, parent/child path, and partiality. A downstream service matching the
search may have a different trace root. Automatic baseline retrieval matches
that root; verify it still identifies a useful comparison for the affected
operation. Keep both operations in scope when needed.

Choose a seed that demonstrates the reported failure mode, and record why.
For heterogeneous symptoms, isolate cohorts rather than explaining all of them
with one extreme trace. Continue with [trace comparison](trace-comparison.md).
