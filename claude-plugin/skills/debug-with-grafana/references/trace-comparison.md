# Baseline candidates and trace diff

Use trace comparison to localize unexplained changes in request execution.
The main skill owns orchestration; this reference owns qualification and
interpretation, not a separate mandatory workflow.

## Check capabilities when entering this path

Reuse known capabilities. If command support is unknown, inspect only the trace
subtree; consult a command's flag help only for a remaining syntax question:

```bash
gcx help-tree traces -o text
```

An older CLI may not expose baseline or diff. Both are experimental; diff is
documented as Grafana Cloud-only. CLI presence does not prove that the selected
backend supports the endpoint. Confirm with the useful bounded operation itself,
not a separate synthetic probe.

An unknown command, unsupported endpoint, permission failure, missing trace,
and expired trace are different failures. Inspect the error before choosing a
fallback. Do not retry an unsupported endpoint or change credentials/policies
automatically. See [error recovery](error-recovery.md).

## Choose the comparison you need

Use the main skill's **question / control / interpretation** test before
retrieving candidates. A comparison should distinguish plausible explanations,
not merely demonstrate that two traces differ.

Choose a control window using existing evidence about the matching population,
not simply "before the alert" or healthy service-wide metrics. A quiet window
may contain little comparable traffic. For intermittent failures, matched
successful requests during the incident may be better controls. If no interval
is known good, call it a candidate comparison window and assess each execution.

Define health using the actual symptom contract and request boundary. A non-error
request can still be slow, and a duration-only filter can exclude valid controls
when an SLO accepts latency **or** throughput. Do not label an execution healthy
from its error count or duration alone.

Keep nuisance dimensions comparable: root/affected operation, environment,
tenant, request shape, data volume, region, cache behavior where observable.
For a rollout hypothesis deliberately vary version while holding other relevant
dimensions stable. Do not match away the suspected cause, such as a new
call path, retries, or fan-out. Record material unknowns rather than assuming
identical operation names imply identical work.

Apply already-verified environment, tenant, and affected-operation constraints
on the first baseline request. Omit unknown or irrelevant constraints; do not
add discovery merely to populate optional filters. Treat remaining workload
dimensions as questions for candidate bodies and diffs, not an exact-match
filter checklist.

Here `$COHORT` is a TraceQL spanset built from verified attributes and values:

```bash
gcx traces baseline --context "$CTX" -d "$TEMPO_UID" "$SEED" \
  --filter "$COHORT" \
  --from "$CONTROL_FROM" --to "$CONTROL_TO" --limit 5 -o agents
```

Omit `--filter` when no additional cohort constraints are known or relevant.
Filters are raw TraceQL spanset expressions, ANDed with the generated query.
Separate spansets may match different spans in one trace; combine conditions
inside one `{ ... }` when they must hold on the same span.

The default candidate window, when no explicit range is supplied, covers the
seed's span range plus 30m on **each** side. It is anchored on the seed, not now,
and can include the same incident. `--window` widens that padding; explicit
`--from` and `--to` override it. Baseline has no `--since` flag. Use the same
context and datasource as the seed.

## Assess candidates iteratively

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
| `startTimeUnixNano`, `durationMs` | Check window; summary duration may cover more than the affected request. Verify the span boundary rather than selecting the fastest outlier. |
| `spanCount`, `serviceCount` | Gross structural context, not similarity scores. Missing counts are unknown. |
| `errorCount` | Reported error spans across services. Missing/null is unknown, not zero. |
| Candidate trace body | Verify operation, context, behavior and completeness when summary metadata cannot establish comparability. |

Root `status != error` includes `unset`; downstream errors are deliberately
retained. Zero reported errors is not proof of health. Conversely, a handled
child error need not disqualify an otherwise appropriate control. Inspect what
happened rather than treating the error count as a boolean health verdict.

Summaries are a shortlist, not the complete qualification evidence. When they
cannot establish comparability, fetch a small batch of plausible candidate
bodies. Independent reads justified by the same question can run in parallel:

```bash
# Apply to each selected candidate, not automatically every search result.
gcx traces get --context "$CTX" -d "$TEMPO_UID" "$CANDIDATE" \
  --llm --from "$CONTROL_FROM" --to "$CONTROL_TO" -o agents
```

Check context and observed health, reject obvious context mismatches, then use
exploratory diffs to assess workload and execution differences. Do not require
every workload dimension to be established before the first diff. Accept or
reject controls using the bodies and diffs together; similarity alone does not
establish health, since two similarly broken requests can look comparable.

Refine filters when this assessment reveals a material mismatch. Do not replace
candidate inspection with increasingly restrictive workload filters merely
because summaries lack detail.

## Handle retrieval bias or missing baseline support

A regression that introduces a dependency can make automatic retrieval require
that dependency in every candidate. Genuine pre-regression controls then fail
the topology fingerprint. Adding `--filter` cannot remove generated constraints;
there is no topology-relaxation flag.

If baseline is unavailable, or its candidates are biased/invalid, use a bounded
same-root-operation search in a justified comparison window **without** pinning
the seed's downstream services:

```bash
gcx traces query --context "$CTX" -d "$TEMPO_UID" \
  "{ trace:rootService = \"$ROOT_SERVICE\" && trace:rootName = \"$ROOT_OPERATION\" } && $COHORT" \
  --from "$CONTROL_FROM" --to "$CONTROL_TO" --limit 5 -o agents
```

Use the seed's verified root service/operation and retain the same `$COHORT`;
omit its conjunction only if no extra scope was needed. This relaxed search
does not assert health: inspect the candidates, exclude the seed itself if
windows overlap, and assess bodies and exploratory diffs as above.

No returned candidates means no match for that scope, filter, and window—not
that healthy executions did not exist. Before another candidate batch, name
the specific mismatch, coverage gap, or retrieval bias the refinement addresses.
If the documented fallback still provides no defensible control, stop the
comparison branch and report the searched scope/window and limitation. Do not
widen windows or remove necessary scope repeatedly just to obtain a pair.

## Diff candidate A against anomalous B

Use A as a prospective baseline while assessing it; passing it first does not
certify it as a healthy control. For example:

- **Question:** Is the request waiting longer for execution, or executing backend work more slowly?
- **Control:** Same affected operation, environment, tenant, and comparable workload; do not constrain away queueing or execution changes.
- **Interpretation:** Longer queue waits support a queueing explanation; slower backend operations need execution or dependency evidence. Workload differences may instead disqualify the candidate.

```bash
# Exploratory diff: PAIR_FROM/PAIR_TO cover both candidate and seed.
gcx traces diff --context "$CTX" -d "$TEMPO_UID" "$CANDIDATE" "$SEED" \
  --from "$PAIR_FROM" --to "$PAIR_TO" -o agents
```

Deltas are **B - A**. Positive duration deltas mean the seed is slower; negative
means faster. A faster request can still be faulty (for example, failing early).
Record what each compared duration measures and its unit.

Diff time flags bound **both** trace lookups, not the comparison cohort. If
adding `--from`/`--to`, cover both the baseline and seed timestamps; an
incident-only bound may exclude an older baseline. Omitting bounds performs a
full lookback. The response is an experimental payload: inspect it rather than
assuming fixed JSON paths or inventing fields it did not return.

Use exploratory diffs to resolve remaining comparability questions and record
why each candidate is accepted or rejected. Only accepted controls support the
final causal explanation; a diff with unresolved material mismatches remains
exploratory. Compare other candidates or anomalous executions when needed to
assess suitability or repeatability. If differences depend on the control,
refine the cohort or report inconclusive evidence. Do not manufacture certainty
by running a fixed quota of diffs.

If server-side diff is unavailable, inspect candidate and seed bodies with
`--llm`, reusing already-retrieved data, and compare operations, dependency
paths, timing, and error evidence manually. Use the same qualification criteria
and report that no server-side diff was available. Do not silently treat
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

Use the affected request boundary for latency claims: whole-trace duration can
include work after its response. Do not add nested/parallel span durations to
estimate a critical path or an additive latency share. Missing spans can reflect
partial collection or instrumentation changes, not a removed dependency.

Use population evidence to measure prevalence and blast radius. TraceQL metrics
can describe the observed trace population when sampling and coverage are
understood, but cannot measure input that was never stored.

## Assess what the comparison contributed

Compare the conclusion with what was known before the comparison. Keep the
examined IDs, reasons for accepting or rejecting controls, relevant deltas and
corroboration. Separate exploratory comparisons from those supporting the final
explanation, and state what changed in the explanation or next action:

| Outcome | What supports it |
| --- | --- |
| New evidence | The comparison revealed an execution difference that changes the explanation or next test; distinguish localization from a corroborated mechanism |
| Confirmation | A defensible control strengthens an existing explanation; name the alternative it weakens |
| No diagnostic gain | The comparison was valid but did not distinguish the explanations or change the next action |
| Inconclusive | Missing/partial traces, invalid controls, or inconsistent differences prevent a defensible conclusion |

No diagnostic gain and inconclusive results are legitimate outcomes, not reasons
to keep querying until the feature appears useful. Do not call an unattempted
comparison inconclusive; say why it was unnecessary or could not be attempted
when that limitation matters. When evaluating the workflow, also record the
comparison's additional reads and elapsed time. Stop once the user's question
is answered.
