---
name: performance-rca
description: >
  Investigates CPU, memory, and contention problems with Pyroscope profiles via
  gcx. Compares affected and baseline workloads, locates changed application
  stacks, and tests root-cause hypotheses against symptom and change evidence.
  Use for high CPU, growing memory, lock contention, hot functions, supplied
  profiling anomaly reports, or code paths that regressed after a deploy.
  Accepts a suspect service, profile/flame graph, or trace from another
  investigation. General incident triage uses debug-with-grafana; alert-rule
  semantics uses investigate-alert and SLO budget analysis uses slo-investigate.
---

# Performance RCA

Explain the observed performance problem: which workload changed, which code
path accounts for it, and what evidence supports the mechanism. A hotspot is a
location; an anomaly is a lead; neither alone proves a root cause. Stop at the
strongest supported conclusion, including an inconclusive result. Code changes
and proactive optimization are separate tasks.

## Investigation contract

1. Follow Sections 1–5 in order. Reuse verified evidence from an earlier step
   or referring investigation; do not repeat discovery merely to satisfy the
   sequence. Establish each step's prerequisites before interpreting its result.
2. Fix the context, datasource, UTC windows, selectors, and profile type before
   comparison. Discover missing identifiers; never invent them. Record any
   justified change to the scope or comparison.
3. Qualify the control in Section 2 before claiming a regression. Without one,
   continue only to current cost attribution and label the comparison missing.
4. Distinguish empty results, unsupported capabilities, and query failures.
   Apply Section 1's exits to the affected profiling path; use its documented
   fallbacks only when their prerequisites hold.
5. Select output formats explicitly: JSON for discovery/metrics, DOT or table
   for profile inspection, and pprof for export. Retain diagnostics and limits.
6. Always produce Section 5's report, including after an exit. Fill unsupported
   fields with `unknown`, `not measured`, or `not applicable`, plus a reason.
   A missing measurement must never become evidence that the service is healthy.

## 1. Preserve the question and check usable profiles

Reuse the context, datasource, service, symptom, incident window, deployment
facts, and trace/profile links supplied by the user or a referring skill. Pin
absolute UTC incident and baseline times. Derive a missing incident interval
from timestamped symptom evidence, or ask for it before querying. Carry the same
`--context <context>` on remote commands (omitted below), and pass the
datasource UID explicitly. Keep the investigation read-only; do not switch
contexts or change collection.

```bash
gcx help-tree profiles -o text
```

Check that the running binary exposes each command, flag, and output format
before using the examples below. These examples target the build that bundles
this skill; an older installed binary may lack time-scoped metadata discovery,
DOT, pprof export, or trace scoping. If required discovery/time flags are
missing, report the version mismatch and use a compatible gcx build before
continuing; do not guess command renames or silently drop time filters.
`data-range` is optional when absent. DOT, pprof, trace scoping, and series have
conditional paths below; skip them when unsupported.

```bash
# Discover only if the datasource UID is unknown; choose the target environment.
gcx datasources list -t grafana-pyroscope-datasource -o json
# If advertised by the running command tree:
gcx profiles data-range -d <pyro-uid> -o json
gcx profiles list-profile-types -d <pyro-uid> \
  --from <incident-from> --to <incident-to> -o json
```

Use the running command tree to check optional capabilities. CLI support does
not imply backend support. `data-range` returns tenant-wide `dataIngested`,
`oldestProfileTime`, and `newestProfileTime` (Unix milliseconds; zero means
unknown). These bounds do not prove that the selected service or lens has
continuous coverage. Do not assume a fixed retention period.

Apply these outcomes before proceeding. Every exit stops the affected profile
queries and produces the Section 5 report; other usable signals may continue
through `debug-with-grafana` with the missing-evidence question made explicit.

- **EXIT A — profiling unavailable:** no target datasource, missing access, or
  a required CLI capability is unavailable. Record the exact limitation; use
  `setup-gcx` for missing setup/access or a compatible build for a CLI mismatch.
- **EXIT B — no ingestion reported:** a successful stats response says
  `dataIngested: false`. Report the instrumentation/data gap,
  not service health.
- **EXIT C — incident not covered:** the incident has no overlap with known
  queryable bounds. Report both ranges; do not replace the incident with today.
  For partial overlap, analyze only that overlap, record the coverage gap, and
  limit conclusions to the observed interval.
- **EXIT D — no usable incident profiles:** after checking the selector, window,
  and lens against discovered facts, incident samples remain absent or zero. A
  missing baseline alone is handled in Section 2, not by this exit. Report the
  unresolved coverage/instrumentation gap, not proof of zero cost.

**Continue with scoped checks** when stats are absent, unsupported, or have
unknown bounds. Missing stats alone must not block usable type/profile queries.
These queries still need matching samples before they can support attribution.

For query errors, retain the operation and diagnostic. Correct a demonstrated
selector/flag mistake; use a documented fallback for unsupported capabilities.
Do not retry authentication failures or vary selectors blindly. A failed query
is not an empty successful result.

## 2. Choose the lens and a qualified comparison

Copy exact profile type IDs from discovery, including their units. Discover
labels rather than assuming Prometheus jobs, Tempo service names, Kubernetes
workloads, and Pyroscope `service_name` have identical values.

| Symptom | Available lens to look for | Interpretation limit |
| --- | --- | --- |
| CPU high or CPU-bound latency | CPU time | Does not measure all request latency, scheduling delay, or throttling |
| Allocation/GC pressure | Allocated bytes/objects | Churn is not retained heap or a memory leak |
| Growing heap/OOM | In-use bytes/objects, alongside allocations | Live-heap samples are snapshots; not process RSS, native memory, or retention ownership |
| Contention or blocked work | Mutex/block delay and counts | Runtime-specific sampling and attribution; not CPU time |
| Suspected goroutine/thread leak | Goroutine/thread counts and stacks | Growth alone is not proof of leaked work |

If the needed lens is absent, record the instrumentation gap; do not substitute
CPU to rule out waiting or allocations to prove a leak.

A baseline must represent expected behavior, not simply precede the incident.
Prefer equal-duration intervals with comparable request mix/rate, replica count,
collection coverage, runtime, and sampling settings. Diurnal workloads may need
a matching earlier time of day. Check profile types and data in the baseline
window too. Compare an unaffected peer when no historical baseline is suitable.
Without a qualified control, report current cost locations without claiming a
regression. A stable but inefficient hot path may still explain a new symptom
under increased load; an unchanged profile shape does not exonerate it.

For a deployment comparison, keep stable workload labels fixed, but select the
old version/pod for the baseline when the incident's version/pod did not exist.
Record both selectors and why they are comparable. Never compare an absent old
pod/version with a populated new one and call it an infinite regression.

An anomaly result can supply the service, lens, onset, affected labels, and a
candidate baseline. Preserve its link, interval, observed/expected values and
score if supplied; validate them with profiles and symptom evidence. Read
[advanced analysis](references/advanced-analysis.md#anomaly-led-entry) for the
anomaly entry path; it requires no anomaly detector to run this skill.

## 3. Localize the change: service → workload slice

Start with the known suspect, rather than a tenant-wide leaderboard. If the
service is unknown, discover `service_name` values with `profiles labels`, then
rank within the known environment; use a tenant-wide query only if necessary.

```bash
gcx profiles labels -d <pyro-uid> \
  --from <incident-from> --to <incident-to> -o json
gcx profiles labels -d <pyro-uid> --label service_name \
  --from <incident-from> --to <incident-to> -o json
# Establish onset and coverage; repeat for the baseline window.
gcx profiles metrics -d <pyro-uid> '{service_name="<suspect>"}' \
  --profile-type <lens> --from <incident-from> --to <incident-to> -o json
# Pick a discovered label that can distinguish affected/unaffected workloads.
gcx profiles metrics -d <pyro-uid> '{service_name="<suspect>"}' \
  --profile-type <lens> --from <incident-from> --to <incident-to> \
  --top --group-by <label> --limit 20 -o json
```

Use `profiles series` to discover actual label combinations for a service
without requiring a profile type. See
[scoped series discovery](references/advanced-analysis.md#scoped-series-discovery)
for label projection and the compatibility fallback for older binaries or
backends that do not support discovery. Tenant-wide label inventories alone do
not prove labels coexist on the suspect.

Repeat the chosen grouping for the baseline. Leaderboards are capped candidates,
not a fleet census; missing groups may have fallen outside the limit. A single
group establishes no peer comparison. Similar totals do not establish uniform
per-instance behavior, and the largest group may simply serve more traffic or
contain more replicas. Use request counts or replica exposure from existing
metrics when needed; if unavailable, state the confounder. Do not use a fixed
ratio threshold to declare a slice anomalous.

Use time-series values to check persistence/onset; `--top` collapses the window.
Keep units and aggregation semantics explicit, especially for live-heap or
count snapshots. Do not sum snapshot values into "bytes allocated" or infer
request-normalized cost from raw profile totals.

## 4. Locate the changed application stack

Run for the incident selector below and the qualified baseline selector/window.
Omit the slice matcher when analyzing the whole service.

```bash
gcx profiles query -d <pyro-uid> \
  '{service_name="<suspect>",<label>="<value>"}' \
  --profile-type <lens> --from <incident-from> --to <incident-to> -o dot
```

Prefer explicit `-o dot` for caller/callee context, self/cumulative values and
source locations when present. On older CLIs use explicit `-o table`; current
CLIs fall back with a warning when the backend explicitly rejects DOT as
unsupported or returns a flame graph instead. Other errors still fail the query.
Inspect the returned format and retain stderr. JSON encodes flame-graph wire
data, not the human function table; agent output may instead reference a spill
file that must be read.

DOT is pruned by default; dotted edges can skip intermediate frames. When
caller context is missing, repeat the same query with `-o dot --max-nodes 250`
in both windows. This raises the node limit; it does not guarantee an unpruned
graph. If attribution remains ambiguous, use the pprof comparison below.

The table shows the top 20 functions ranked by SELF and truncates names to 60
characters. Its PERCENTAGE column is SELF divided by the profile total, not the
function TOTAL share. Neither format can rule out a missing path. For ambiguous
attribution or numerical differences, read
[pprof comparison](references/advanced-analysis.md#pprof-comparison) and export
both profiles. Do not assume an exported profile repairs collection gaps.

Interpret the evidence with these constraints:

- Compare absolute cost and share in each window, with units and a denominator.
  Equal shares can hide doubled cost; larger shares can reflect other work
  disappearing. Percentages are not request latency or proof of regression.
- Self/flat means work at that frame; total/cumulative includes descendants.
  Use caller/callee context to locate a meaningful application path. Ancestor
  totals overlap: do not add them or assign runtime work to a guessed caller.
- Runtime symbols suggest mechanisms to test. `mallocgc` suggests allocation
  work; GC workers may run separately from allocation sites; `nanotime` alone
  does not prove busy-waiting. CPU samples in `futex`/locks do not measure time
  asleep. Confirm waiting with a supported wait lens and other evidence.
- In Go, heap stacks identify allocation sites, not the object retaining
  references. Mutex profiles attribute contention to the holder/unlock path;
  block profiles to the waiter. Verify the actual runtime semantics before
  transferring this interpretation to another language or collector.

When a matching checkout/build is available, inspect the implicated function to
validate the mechanism. Read profile locations and verified deployment revision
metadata; do not assume a Build ID is a Git SHA or that `service_repository`
always exists. Report file:line only when symbolization and revision support it;
otherwise retain function-level attribution and say what is missing. Do not
edit source as part of this investigation.

## 5. Test the cause and report

Carry the suspect path back to the original symptom. Use the relevant evidence
through `debug-with-grafana`: request/CPU/heap trends, throttling, queueing,
dependency latency, logs, configuration or deployment changes. Reuse existing
findings; query only what can distinguish the leading explanation from a
material alternative. For a supplied request, see
[trace correlation](references/advanced-analysis.md#trace-correlation).

A deployment timestamp or detector score is correlation. Strengthen a causal
claim with matching onset, a specific mechanism, affected/unaffected controls,
and repeatable differences or an already-observed recovery. Do not perform a
rollback or load test merely to validate the hypothesis.

Stop when the user's question is answered or missing evidence prevents further
attribution. Return a root cause only when the mechanism and symptom connection
are supported; otherwise return a suspected cause, cost location, or
inconclusive result. Never turn "not observed in these profiles" into
"not in this service".

Use this report for every outcome. Keep the field names and order; add details
under Evidence only when needed to support the conclusion. A blocked or empty
investigation is `inconclusive`, with the exit and missing evidence recorded.
Without a qualified baseline, report measured cost locations without regression
claims. Unknown fields remain explicit; do not fill them with guessed values.

```text
Conclusion:     <established cause | suspected cause | cost location |
                 inconclusive> — <answer to the original question>
Exit:           <A | B | C | D | none> — <reason, if applicable>
Scope:          <context, datasource UID, service>
Lens:           <exact profile type and units>
Incident:       <UTC from/to; exact selector; observed coverage>
Baseline:       <UTC from/to; exact selector; or unavailable with reason>
Control:        <why comparable; workload/collection differences or gaps>
Slice:          <affected label/value; evidence of scope or unknown>
Location:       <application stack; file:line and revision only if verified>
Cost:           <incident vs baseline absolute values, units, aggregation>
Share:          <incident vs baseline; self/cumulative and denominator>
Normalization:  <method and measured denominator | not applied, with reason>
Mechanism:      <supported explanation or explicitly labeled hypothesis>
Evidence:       <profile artifacts/links and corroborating symptom evidence>
Counterevidence: <alternatives checked and results | not checked, with reason>
Confidence:     <high | medium | low> — <reason tied to available evidence>
Limitations:    <coverage, sampling, baseline, symbolization or access gaps>
Next action:    <smallest missing evidence or handoff question | none>
```
