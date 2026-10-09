# Bounded error recovery

Recover only when it advances the current question. Do not repeatedly broaden
queries, change context, or fix infrastructure merely to complete a signal
checklist. Preserve the original scope and record what remains unverified.

## Authentication, authorization, or wrong target

```bash
gcx config current-context
gcx config view --context <context> --minify -o json
gcx config check --context <context>
```

Check **only the selected context**; a bare config check checks all contexts.
A 401 can mean missing/expired/wrong credentials; a 403 can mean missing
permission for a specific datasource or endpoint. Neither means the application
has no telemetry. Use another already-authorized signal when it can answer the
question. Ask for the smallest required access only when blocked.

Do not expose secrets with `--raw`, print full HTTP payloads, rotate credentials,
or switch the persisted context automatically. Use `setup-gcx` for an explicit
setup request. An inaccessible experimental endpoint is not automatically an
unsupported endpoint.

## Datasource not found

Use the context or dashboard's datasource UID; do not reuse a UID from another
stack or choose the first entry in a list. Resolve ambiguity with type/name:

```bash
gcx datasources list --context <context> --type tempo --name <environment> \
  --limit 20 --json uid,name,type
gcx datasources get <uid> -o json
```

Use the selected context consistently on subsequent commands. A datasource
listing confirms configuration, not backend health or incident coverage.

## Empty or incomplete results

Distinguish empty data, a query error, access failure, and a limited sample.
Empty is not zero, disabled sampling, or proof of a healthy/down application.

1. Recheck target, selector, time zone, and requested window against supplied
   evidence. Ensure time bounds cover the trace being retrieved.
2. Check actual schema/retained labels and a known working query. Do not assume
   `job`, `status`, or `service.name` are the right keys.
3. Check returned timestamps, spacing, gaps, warnings, and known retention or
   sampling. Metadata presence is not historical coverage.
4. Relax one justified constraint or expand to one meaningful comparison window.
   Preserve tenant/environment scope. Do not drift to another population just
   to obtain non-empty output.
5. If still insufficient, change signal or report the evidence gap and stop.

For Loki, only indexed labels belong inside `{...}`. Put structured metadata
after a pipe and parsed fields after their parser; see
[query patterns](query-patterns.md). A default log query returns at most 50
lines in the current CLI, not a frequency or complete first-occurrence history.

For aggregated metrics, reuse a supported aggregation and inspect retained
labels. Labels aggregated away cannot establish the requested scope. A rejected
raw selector is not proof the metric is absent. If requested and returned
resolution differ, report actual coverage; do not assume which layer caused it.

## Timeout, rate limit, or server failure

Narrow time and indexed selectors, reuse a recording rule, or reduce resolution
where that still answers the question. Field selection and output limits may
reduce tokens without reducing backend scans. Aggregation does not guarantee
that intermediate series limits disappear.

For 429, respect retry guidance/backoff. Do not repeatedly retry an expensive
query against an overloaded backend. For persistent 5xx, record the failure and
use another useful signal; do not convert it into "no data". Test connectivity
with a small relevant read only if the distinction matters.

## Query syntax or CLI mismatch

- Check the running command's help before guessing flags. Signal query commands
  take one expression positional argument; put the datasource UID in `-d`.
- Prometheus: use valid label matchers and range windows on counters. `--time`
  is valid for instant `increase()`/`avg_over_time()` queries and cannot combine
  with `--from`/`--to`/`--since`.
- Loki: include a non-empty indexed matcher, use the right parser, and filter
  `__error__` after error-producing stages. Use `gcx logs metrics` for aggregate
  LogQL, not the log-line response path.
- Tempo: use scoped resource/span attributes and unquoted status/kind enums.
  Inspect attribute types; an HTTP code may not be encoded as assumed.
- JSON piping: keep stderr separate, inspect exit status, and retain warnings.
  Use `--json list` to discover fields instead of guessing paths.

## Baseline or diff cannot run

Use [trace comparison](trace-comparison.md) for the fallback protocol:

| Failure | Recovery |
| --- | --- |
| Baseline command/required TraceQL capability unavailable | Same-operation bounded search, then assess candidate bodies and exploratory diffs |
| No valid controls or topology-biased retrieval | Relax generated topology constraints via ordinary TraceQL search, not an invented flag |
| Partial seed or missing root | Try a complete representative seed; time overrides cannot restore missing structure |
| Diff endpoint unavailable | Reuse or fetch candidate and seed bodies with `--llm`; assess comparability and execution differences manually, and disclose the limitation |
| Trace lookup not found | Check ID, datasource, retention, and time bounds before assuming the endpoint is absent |
| Comparisons disagree | Refine the cohort or report inconclusive evidence; do not select the diff that best fits a preferred explanation |

Unsupported tooling must not block an otherwise answerable question. Conversely,
if request-level evidence is essential and unavailable, say what cannot be
established rather than substituting a fabricated root cause.
