# From an alert or symptom to a representative trace

Use this reference when request execution remains unexplained and you need a
seed for baseline comparison. A supplied trace ID skips localization: confirm
its target, fetch it, and check that it represents the stated symptom.

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
gcx alert rules get <rule-uid> -o json
```

If the rule UID is unknown and current firing state is relevant, narrow by a
known folder/group before inspecting a bounded list:

```bash
gcx alert rules list --folder <folder-uid> --state firing --limit 20 -o json
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

1. Follow a supplied trace ID/link after confirming context and datasource.
2. Follow a metric exemplar relevant to the operation and incident interval.
   Use an existing exemplar ID/link; do not invent a gcx exemplar command.
3. Search Tempo using the verified service, operation, interval, and symptom.
4. Use a log trace ID if it supplies the missing connection. Do not scan logs
   merely to earn access to traces.

Example search after discovering the schema (replace placeholders):

```bash
# Duration here is server-span duration, not total trace duration.
gcx traces query -d "$TEMPO_UID" \
  '{ resource.service.name = "<service>" && name = "<operation>" && kind = server && duration > <threshold> }' \
  --from "$FROM" --to "$TO" --limit 10 -o json

# For an HTTP-error symptom, use the actual response-status attribute/type.
gcx traces query -d "$TEMPO_UID" \
  '{ resource.service.name = "<service>" && name = "<operation>" && span.http.response.status_code >= 500 }' \
  --from "$FROM" --to "$TO" --limit 10 -o json
```

Search returns exemplars, not a ranked or statistically representative sample.
A cap of 10 is a starting bound, not a completeness guarantee. Narrow/refine the
cohort when results are unrelated; do not fetch every trace or automatically
select the first result or slowest outlier.

```bash
gcx traces get -d "$TEMPO_UID" "$SEED" --llm \
  --from "$FROM" --to "$TO" --share-link -o json
```

Inspect the body for the affected operation, duration/error evidence, relevant
context, parent/child path, and partiality. A downstream service matching the
search may have a different trace root. Automatic baseline retrieval matches
that root; verify it still identifies a useful comparison for the affected
operation. Keep both operations in scope when needed.

Choose a seed that demonstrates the reported failure mode, and record why.
For heterogeneous symptoms, isolate cohorts rather than explaining all of them
with one extreme trace. Continue with [trace comparison](trace-comparison.md).
