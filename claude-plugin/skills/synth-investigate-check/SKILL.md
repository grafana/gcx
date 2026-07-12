---
name: synth-investigate-check
description: Diagnoses why a Synthetic Monitoring check is failing - triages probe failures, classifies failure scope, runs per-probe breakdown, and identifies root cause. Use when the user wants to investigate a failing check. Trigger on phrases like "why is my check failing", "investigate synthetic check", "probe failures", "check is down". For check status overview use synth-check-status. For creating or managing checks use synth-manage-checks.
allowed-tools: Bash
---

# Synthetic Check Investigator

Investigate Synthetic Monitoring check failures by triaging probe data, classifying failure scope, and identifying root cause.

## Core Principles

1. Use gcx commands — do not call Grafana APIs directly
2. Trust the user's expertise — skip background explanations
3. Use `-o json` for agent processing, default format for user display
4. Show timeline graphs for time-series data — they communicate trends faster than text
5. Collect errors; report them at the end, not interleaved in workflow steps

## Investigation Workflow

### Step 1: Get Check Status (with early exit)

```bash
gcx synthetic-monitoring checks status <ID>
```

If the user provided a name instead of ID, list first with a job glob (the numeric ID is the NAME suffix, e.g. `web-check-1001` is ID `1001`):
```bash
gcx synthetic-monitoring checks list --job '*<name>*'
```

**Early exit — OK:** Status command reports `OK` (success rate at or above the check's alertSensitivity threshold: high = 95%, medium/default = 90%, low = 75%).
Report: "Check `<job>` is healthy. Success rate: <rate>%. <probe_count> probes up."
Stop unless the user asks for more.

**Early exit — NODATA:** No Prometheus metrics available.
1. Get check config to verify `enabled: true` (`gcx synthetic-monitoring checks get <ID> -o json | jq .spec.enabled`)
2. If disabled: report "Check is disabled — no metrics will appear until it is re-enabled."
3. If enabled: report "No metrics found. Check datasource config or whether the SM stack is healthy."
Stop after reporting.

### Step 2: Get Check Configuration

```bash
gcx synthetic-monitoring checks get <ID> -o json
```

Extract: job name, target, check type (http/ping/dns/tcp/traceroute), probe list, frequency, timeout, alertSensitivity, enabled flag.

For HTTP checks also note: any assertion settings, TLS config, expected status codes.

### Step 3: Timeline Triage

```bash
gcx synthetic-monitoring checks timeline <ID> --from now-1h --to now
```

Show the graph output to the user. Then analyze the pattern:

| Pattern | Classification |
|---------|---------------|
| All probes at 0 (or near 0) | Target down |
| Subset of probes at 0, others healthy | Regional / network |
| Intermittent drops across multiple probes | Flapping / timeout |
| All probes drop at a specific point in time | Sudden onset — possible deployment or config change |
| Gradual decline | Degradation — timeout drift or resource exhaustion |

Use a longer window if the failure started more than 1h ago:
```bash
gcx synthetic-monitoring checks timeline <ID> --from now-6h --to now
```

### Step 4: Classify Failure Scope and Map Probes

Get the probe list for geographic mapping:
```bash
gcx synthetic-monitoring probes list -o json
```

Cross-reference the probe names from the check config against each probe's `region` field. Map failing probes to their regions.

**All probes failing:** Target/service issue — likely target down, SSL error, or DNS failure.

**Subset of probes failing:** Regional or network issue. Note which regions are affected:
- Single region → ISP/CDN routing issue or regional outage
- Multiple contiguous regions → CDN edge or routing policy issue
- Probe-specific → private probe infra issue (if using private probes)

**Intermittent failures:** Flapping. Consider: rate limiting, timeout too tight, flaky connectivity.

### Step 5: Per-Probe Breakdown via PromQL (when datasource is available)

Resolve datasource UID if not already known:
```bash
gcx datasources list --type prometheus
```

If the filtered list comes back empty, the stack may leave the `type` field
blank in list payloads (known issue) — rerun without `--type` and pick the
Prometheus datasource by name.

Run per-probe success rate to pinpoint failing probes (use `-o json` for parsing, `-o graph` to show the user):
```bash
gcx metrics query -d <datasource-uid> \
  'avg by (probe) (probe_success{job="<job>",instance="<target>"})' \
  --from now-1h --to now --step 1m -o graph
```

For HTTP checks, also run HTTP phase latency to locate where time is spent:
```bash
gcx metrics query -d <datasource-uid> \
  'avg by (phase) (probe_http_duration_seconds{job="<job>",instance="<target>"})' \
  --from now-1h --to now --step 1m -o graph
```

For SSL/TLS cert expiry, DNS latency, per-probe error rates, and other patterns, see [sm-promql-patterns.md](references/sm-promql-patterns.md).

### Step 6: Classify Failure Mode

Cross-reference signals against [failure-modes.md](references/failure-modes.md) (full signal/cause/next-action table and decision tree) to select the most likely failure mode:

1. All probes failing + HTTP non-2xx or connection refused → **Target down**
2. Subset of probes failing → **Regional/CDN**
3. TLS handshake error or cert expiry < 14 days → **SSL/TLS**
4. DNS resolution errors across probes → **DNS resolution**
5. All probes timing out, phase latency high in `connect` or `tls` → **Timeout**
6. Probes reaching target but assertion fails (status code, body match) → **Content/assertion**
7. Single private probe failing, public probes healthy → **Private probe infra**
8. HTTP 429 responses, intermittent failures with backoff pattern → **Rate limiting**

### Step 7: Diagnosis and Next Actions

Synthesize findings into an actionable report (see Output Format below). Take next actions from the "Next Action" column of the failure mode's row in [failure-modes.md](references/failure-modes.md).

If deeper investigation is needed (e.g., logs, infra repos), ask the user if they want to proceed.

If check config needs changes (probe selection, frequency, assertions), route to **synth-manage-checks**.

## Output Format

**Early exit (OK):**
```
Check: <job> (<target>)
Status: OK
Success rate: <rate>%
Probes up: <count>/<total>
```

**Early exit (NODATA):**
```
Check: <job> (<target>)
Status: NODATA
Enabled: <yes/no>
Next: <datasource check / re-enable instruction>
```

**Full investigation:**
```
Check: <job> (<target>)
Type: <http|ping|dns|tcp|traceroute>
Status: FAILING
Success rate: <rate>% (window: <from> – <to>)

[Timeline graph]

Failure classification: <Target down | Regional/CDN | SSL/TLS | DNS | Timeout | Content/assertion | Private probe infra | Rate limiting>

Affected probes: <count>/<total>
  - <probe-name> (<region>): failing since <time>
  - <probe-name> (<region>): intermittent

Onset: <time/duration or "unknown">

Diagnosis:
<2-4 sentences describing what the data shows and the most likely cause>

Next actions:
1. <action>
2. <action>
3. <action>
```

Use minimal formatting. Avoid excessive bold text. Trust the user to prioritize.

## Error Handling

- `gcx synthetic-monitoring checks status` returns no rows: check ID may be wrong — list all checks and confirm
- `gcx synthetic-monitoring probes list` fails: skip geographic mapping; classify probes by name where possible
- `gcx metrics query` fails with datasource error: note it, skip PromQL steps, classify using timeline data only
- Multiple checks match the search name: list all with IDs and targets, ask which to investigate
- Timeline returns no data for the window: widen to `--from now-6h --to now` before concluding NODATA
