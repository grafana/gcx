# Baseline candidates and trace diff

Use trace comparison to localize unexplained changes in request execution.
The main skill owns orchestration; this reference owns qualification and
interpretation, not a separate mandatory workflow.

## Check capabilities when entering this path

```bash
gcx version
gcx help-tree traces -o text
gcx traces baseline --help
gcx traces diff --help
```

Inspect the command tree first; an older CLI may not expose baseline or diff.
Both are experimental; diff is documented as Grafana Cloud-only. CLI presence
does not prove that the selected backend supports the endpoint. Confirm with
the useful bounded operation itself, not a separate synthetic probe.

An unknown command, unsupported endpoint, permission failure, missing trace,
and expired trace are different failures. Inspect the error before choosing a
fallback. Do not retry an unsupported endpoint or change credentials/policies
automatically. See [error recovery](error-recovery.md).

## Choose the comparison you need

Prefer a known-good interval supported by independent evidence, not simply
"before the alert". For intermittent failures, matched successful requests in
the same interval may be better controls. For latency, a non-error request can
still be slow. If no interval is known good, call it a candidate comparison
window and assess behavior before labeling any execution healthy.

Keep nuisance dimensions comparable: root/affected operation, environment,
tenant, request shape, data volume, region, cache behavior where observable.
For a rollout hypothesis deliberately vary version while holding other relevant
dimensions stable. Do not match away the suspected cause, such as a new
call path, retries, or fan-out. Record material unknowns rather than assuming
identical operation names imply identical work.

```bash
gcx traces baseline -d "$TEMPO_UID" "$SEED" \
  --from "$GOOD_START" --to "$GOOD_END" --limit 20 -o json
```

Start without custom `--filter`. The default candidate window, when no explicit
range is supplied, covers the seed's span range plus 30m on **each** side. It is
anchored on the seed, not now, and can include the same incident. `--window`
widens that padding; explicit `--from` and `--to` override it. Baseline has no
`--since` flag. Use the same datasource as the seed.

## Qualify, do not blindly select

Baseline returns candidates in search order, **not a ranking**. It matches root
service/operation, requires a non-error root operation, and pins up to three of
the seed's busiest downstream services. These are retrieval heuristics, not
proof of health or identical execution.

Inspect:

| Evidence | Meaning / action |
| --- | --- |
| `seedPartial` | Retrieval used incomplete seed spans; missing structure may bias the query. Prefer a complete representative seed if available. |
| `list_meta.truncated` | More candidates exist; neither this page nor its first item is a best-match claim. Refine scope or increase the limit only when useful. |
| `query` | Inspect the generated constraints, especially when controls seem implausible or no candidates appear. |
| `startTimeUnixNano`, `durationMs` | Check window and symptom; do not select the fastest outlier. |
| `spanCount`, `serviceCount` | Gross structural context, not similarity scores. Missing counts are unknown. |
| `errorCount` | Reported error spans across services. Missing/null is unknown, not zero. |
| Candidate trace body | Verify operation, context, behavior and completeness when summary metadata cannot establish comparability. |

Root `status != error` includes `unset`; downstream errors are deliberately
retained. Zero reported errors is not proof of health. Conversely, a handled
child error need not disqualify an otherwise appropriate control. Inspect what
happened rather than treating the error count as a boolean health verdict.

Fetch candidate bodies as needed with `gcx traces get -d <tempo-uid> <candidate-id> --llm -o json`.
If candidates differ on a material context dimension, refine with a verified
attribute, then reassess:

```bash
gcx traces baseline -d "$TEMPO_UID" "$SEED" \
  --from "$GOOD_START" --to "$GOOD_END" --limit 20 \
  --filter '{ resource.service.name = "<service>" && span.tenantID = "<tenant>" }' -o json
```

Filters are raw TraceQL spanset expressions, ANDed with the generated query.
Separate spansets may match different spans in one trace; combine conditions
inside one `{ ... }` when they must hold on the same span. Use observed
attribute names, not the illustrative `tenantID` without discovery.

## Handle retrieval bias or missing baseline support

A regression that introduces a dependency can make automatic retrieval require
that dependency in every candidate. Genuine pre-regression controls then fail
the topology fingerprint. Adding `--filter` cannot remove generated constraints;
there is no topology-relaxation flag.

If baseline is unavailable, or its candidates are biased/invalid, use a bounded
same-root-operation search in a justified comparison window **without** pinning
the seed's downstream services:

```bash
gcx traces query -d "$TEMPO_UID" \
  '{ trace:rootService = "<root-service>" && trace:rootName = "<root-operation>" }' \
  --from "$GOOD_START" --to "$GOOD_END" --limit 20 -o json
```

Retain necessary environment/tenant scope using discovered attributes. This
relaxed search does not assert health: inspect the candidates, exclude the seed
itself if windows overlap, and qualify as above. Do not interpret no candidates
as proof that no healthy executions existed; retention/sampling may prevent
retrieval. Stop or state the limitation when no defensible control exists.

## Diff baseline A against anomalous B

```bash
gcx traces diff -d "$TEMPO_UID" "$BASELINE" "$SEED" -o json
```

Deltas are **B - A**. Positive duration deltas mean the seed is slower; negative
means faster. A faster request can still be faulty (for example, failing early).
Record what each compared duration measures and its unit.

Diff time flags bound **both** trace lookups, not the comparison cohort. If
adding `--from`/`--to`, cover both the baseline and seed timestamps; an
incident-only bound may exclude an older baseline. Omitting bounds performs a
full lookback. The response is an experimental payload: inspect it rather than
assuming fixed JSON paths or inventing fields it did not return.

Start with a qualified comparison. Repeat against other candidates and/or
anomalous executions when the claim requires repeatability. If differences
change with the control, refine the cohort or report inconclusive evidence.
Do not manufacture certainty by running a fixed quota of diffs.

If server-side diff is unavailable, fetch both qualified traces with `--llm`
and compare their operations, dependency paths, timing, and error evidence
manually. Report that no server-side diff was available. Do not silently treat
ordinary JSON text differences as an execution diff. Backends may return the
standard trace encoding despite `--llm`; inspect the actual response.

## Turn the difference into a testable explanation

- **More calls/fan-out:** check attempt/retry attributes and targeted logs,
  downstream errors, and retry/configuration changes. Repeated spans alone do
  not prove retries; they can be parallel work.
- **Same path, longer spans:** inspect that dependency's latency, resource
  pressure, queueing, or locks. Parent elapsed time includes child waiting.
- **New failure:** correlate the failing operation's error details and changes.
- **Rollout:** test old/new versions on comparable work and examine unaffected
  cohorts. A version attribute and temporal coincidence alone do not prove cause.

Do not add nested/parallel span durations to estimate a critical path or an
additive latency share. Missing spans can reflect partial collection or
instrumentation changes, not a removed dependency.

Use population evidence to measure prevalence and blast radius. TraceQL metrics
can describe the observed trace population when sampling and coverage are
understood, but cannot measure input that was never stored. Report the selected
IDs, qualification, consistent deltas, corroboration, and remaining uncertainty;
stop once the user's question is answered.
