# Conditional profiling analysis

Use only the section needed for the next diagnostic question. The main skill
owns comparison qualification and evidence limits; those apply here too.

## Scoped series discovery

Use `profiles series` to discover label sets scoped to the suspect service and
time window. Select the returned labels with `--label-name`.

For compatibility with older binaries, check `gcx help-tree profiles -o text`
and the command's help for `series` and `--label-name` support. If either is
missing, use the fallback below.

Substitute label names discovered by `profiles labels`; the placeholders do
not assert that a pod or version label exists.

```bash
gcx profiles series -d <pyro-uid> '{service_name="<suspect>"}' \
  --label-name service_name --label-name <slice-label> \
  --from <incident-from> --to <incident-to> -o json
```

Project only the labels needed for the next comparison to avoid fetching every
high-cardinality label set. Projection is not a row limit and loses omitted
dimensions; projected rows are not a replica count. Include both dimensions
when checking coexistence. Repeat for the baseline to find label churn or the
prior version; the endpoint discovers inventory, not resource cost or anomalies.
Multiple `--match` selectors are a union, not an intersection. Put constraints
in the same selector when they must all hold.

If the command/backend is unavailable, use `profiles labels` for names/values,
then scoped `profiles metrics --group-by <label>` as in the main skill. Only
returned scoped groups establish that a label/value belongs to this workload.
Missing combinations remain unknown. Avoid a full-tenant series dump just to
find one service.

## Pprof comparison

Export when DOT/table pruning, collapsed caller contexts, or unclear numerical
changes prevent attribution, and the running CLI advertises `-o pprof` and
`--pprof-path`. Otherwise retain the supported profile view and report the
limit.
Use a fresh local artifact directory and record the selectors/windows alongside
the files. Copying the commands below requires
substituting the qualified selectors; baseline pods/versions may differ.

```bash
gcx profiles query -d <pyro-uid> '<incident-selector>' \
  --profile-type <lens> --from <incident-from> --to <incident-to> \
  -o pprof --pprof-path <artifact-dir>/incident.pb.gz
gcx profiles query -d <pyro-uid> '<baseline-selector>' \
  --profile-type <lens> --from <baseline-from> --to <baseline-to> \
  -o pprof --pprof-path <artifact-dir>/baseline.pb.gz

go tool pprof -top -nodecount=0 -nodefraction=0 -edgefraction=0 \
  <artifact-dir>/incident.pb.gz
go tool pprof -top -nodecount=0 -nodefraction=0 -edgefraction=0 \
  <artifact-dir>/baseline.pb.gz
go tool pprof -top -cum -nodecount=0 -nodefraction=0 \
  -base <artifact-dir>/baseline.pb.gz <artifact-dir>/incident.pb.gz
go tool pprof -lines -top -cum <artifact-dir>/incident.pb.gz
```

The pprof export defaults to no node limit, but contains only collected data.
Check the sample type and unit printed by pprof; select the matching
`-sample_index` if the file contains multiple sample types. All compared files
must use the same type and unit. Positive baseline-subtracted values indicate
growth, negative values a reduction; inspect both signs, not just the first row.
Use each original profile for shares, since diff percentages use a different
denominator. `-nodefraction=0` alone does not disable the displayed node count.

Raw subtraction compares measured totals. For CPU/allocations, equal duration
is insufficient when traffic, replica exposure, or collection changed. For
in-use heap, compare equivalent snapshots/aggregation rather than dividing a
snapshot by elapsed time. If request-normalized cost is needed, use a measured
request denominator matching the profile scope; do not invent one.

Pprof `-normalize` equalizes profile totals before subtraction: it compares
composition, not cost per request, and can hide a proportional increase in all
functions. Use it only for a clearly stated shape question alongside the raw
values. Missing/zero baselines do not support a ratio or normalized comparison.

If pprof is unavailable or symbols are missing, retain the DOT/table evidence
and state the attribution limit. Source lines can come from the profile itself;
reading local code additionally requires a matching repository and revision.

## Trace correlation

Use when a user or triage provides a trace associated with the symptom and the
running CLI advertises the exemplar/query commands and scoping flags below.
If it lacks DOT, use `-o table`; if it lacks trace scoping, retain only service
window attribution. Do not
infer a trace ID from a span ID. Scoped profiles require matching
instrumentation
and collected samples; a slow span can contain mostly unprofiled waiting.

```bash
gcx profiles exemplars span -d <pyro-uid> '{service_name="<suspect>"}' \
  --profile-type <lens> --from <incident-from> --to <incident-to> -o json
gcx profiles query -d <pyro-uid> '{service_name="<suspect>"}' \
  --profile-type <lens> --from <incident-from> --to <incident-to> \
  --trace-id <trace-id> -o dot
```

Use trace IDs from supplied traces or exemplars that actually contain them.
Exemplars with other span IDs do not establish coverage for the target request.
For a known span use `--span-id <span-id>` with `-o table`; span scoping cannot
be combined with DOT/pprof or trace scoping. Empty results mean no matching
collected samples, not a cheap trace. If unsupported, retain the service-window
profile but explicitly give up request-specific attribution.

For a request regression, reuse `debug-with-grafana` to qualify baseline traces
and compare their execution; keep those controls separate from fleet profile
baselines. Profile CPU totals and overlapping span durations are not additive
parts of end-to-end latency.

## Anomaly-led entry

This skill does not fetch or run an anomaly detector. No profiling anomaly API
or production availability is assumed. Anomaly results can prioritize **where
and when to investigate**. Accept a
supplied detector result as another entry alongside symptoms, alerts and traces:

```text
Anomaly candidate → verify scope/onset and collection → qualify a control
→ compare affected/unaffected profile stacks → corroborate the mechanism
→ supported cause or explicit uncertainty
```

A detector's baseline/score is a proposal, not a known-good control or causal
confidence. Check traffic/replica changes, sampling or collector changes,
missing data, label churn, and deployment timing before interpreting a change
as code regression. Inspect the original profile signal when accessible.

An internal UI link alone does not define an API contract. If inaccessible,
record that limitation and use the supplied service/window with normal profile
queries. Do not invent anomaly commands, scrape private admin endpoints, embed
tenant-specific URLs in this portable skill, or claim a future integration is
committed. A native integration needs a supported API and confirmed ownership,
auth, output semantics, and availability; this workflow does not depend on it.

## Interpretation references

Consult when runtime semantics or comparison meaning needs clarification:

- [Pyroscope profiling types](https://grafana.com/docs/pyroscope/latest/introduction/profiling-types/).
- [Go runtime profiles](https://pkg.go.dev/runtime/pprof): heap, mutex, and block attribution.
- [Pprof comparison options](https://github.com/google/pprof/blob/main/doc/README.md#comparing-profiles).
- [Pyroscope trace/span profiles](https://grafana.com/docs/pyroscope/latest/configure-client/trace-span-profiles/).
