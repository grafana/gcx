---
name: synth-check-status
description: Use when the user asks about Synthetic Monitoring check health, status, or trends. Trigger on phrases like "are my checks healthy", "check status", "synth check", "probe status", or when users mention specific check names or IDs. For investigating failing checks use synth-investigate-check. For creating or managing checks use synth-manage-checks.
allowed-tools: [gcx, Bash]
---

# Synthetic Monitoring Check Status

View check health, status, and timelines. Concise and direct — experienced operators need actionable status, not hand-holding.

## Core Principles

1. Use gcx commands exclusively — do not call APIs directly
2. Trust the user's expertise — no excessive explanation
3. Use `-o json` for agent processing, default format for user display
4. Show graph visualizations for time-series data

## Workflow

### Step 1: List All Checks

Always start with the full check inventory:

```bash
gcx synth checks list
```

Output columns: ID, JOB, TARGET, TYPE. Identify the check(s) the user is asking about.

If the user specifies a check name or target, filter after listing:
```bash
gcx synth checks list -o json | jq '.[] | select(.spec.job == "my-check")'
```

### Step 2: Show Status

For all checks (overview):
```bash
gcx synth checks status
```

For a specific check:
```bash
gcx synth checks status <ID>
```

Output columns: ID, JOB, SUCCESS%, STATUS, PROBES_UP.

**Status interpretation:**
- `OK` — success rate >= 50%; check is healthy
- `FAILING` — success rate < 50%; more than half of probes failing; route to synth-investigate-check for deeper analysis
- `NODATA` — no Prometheus data; check may be disabled or datasource misconfigured

### Step 3: Conditional Timeline

Show timeline when:
- User asks about trends or history
- Any check shows `FAILING` status

With a duration shorthand:
```bash
gcx synth checks timeline <ID> --since <duration>
```

With an explicit time range:
```bash
gcx synth checks timeline <ID> --from <start> --to <end>
```

Examples:
```bash
gcx synth checks timeline 42 --since 1h
gcx synth checks timeline 42 --since 6h
gcx synth checks timeline 42 --from now-24h --to now
gcx synth checks timeline 42 --from 2026-01-01T00:00:00Z --to 2026-01-02T00:00:00Z
```

Note: `--since` and `--from`/`--to` are mutually exclusive. Use one or the other.

**Timeline pattern interpretation:**
- Flat line at 1.0 — healthy; all probes succeeding consistently
- Drops to 0.0 — probe-level failures; investigate with synth-investigate-check
- Intermittent spikes to 0 — flapping; transient failures or timeout issues
- Gradual decline — degrading target or increasing probe timeouts

### Step 4: Routing and Next Actions

After presenting status:

- If any check is `FAILING`: suggest `synth-investigate-check` for per-probe breakdown, failure mode classification, and PromQL deep-dive
- If any check is `NODATA`: note that no Prometheus data is available; suggest checking if the check is enabled and verifying datasource configuration
- If user wants to create, update, or delete checks: route to `synth-manage-checks`

## Output Format

For status overview (all checks healthy):
```
Checks: <N> total, <N> OK, <N> FAILING, <N> NODATA

[Table from gcx synth checks status]

All checks healthy.
```

For status with FAILING checks:
```
Checks: <N> total, <N> OK, <N> FAILING, <N> NODATA

[Table from gcx synth checks status]

FAILING checks:
- <ID> <JOB> (<TARGET>) — <SUCCESS%> success, <PROBES_UP> probes up

[Timeline graph if shown]

For per-probe breakdown and failure diagnosis, use synth-investigate-check.
```

For a specific check with timeline:
```
Check: <ID> <JOB> (<TARGET>) [<TYPE>]
Status: <OK|FAILING|NODATA>
Success: <SUCCESS%>
Probes up: <PROBES_UP>

[Timeline graph]

Timeline pattern: <flat/drops/intermittent/declining>
```

For NODATA checks:
```
Check: <ID> <JOB> (<TARGET>)
Status: NODATA — no Prometheus metrics available.

Possible causes:
- Check is disabled (spec.enabled: false)
- Synthetic Monitoring datasource not configured
- Metrics not yet available (newly created check)

Verify: gcx synth checks status <ID> -o json | jq '.spec.enabled'
```

## Error Handling

- **`gcx synth checks list` returns empty**: No checks configured in this context. Verify the gcx context with `gcx config view`.
- **`gcx synth checks status <ID>` fails with "not found"**: Confirm the ID from `gcx synth checks list`. IDs are numeric.
- **`gcx synth checks timeline <ID>` fails**: Verify the ID exists and that the check has been running long enough to have data. New checks may show no timeline data.
- **`--since` and `--from`/`--to` both provided**: These flags are mutually exclusive. Use one or the other.
- **Timeline shows no data for the selected range**: Try a longer duration (e.g., `--since 24h` instead of `--since 1h`). The check may have been created recently.
- **Context not set**: Run `gcx config view` to verify the active context. If multiple contexts exist and none specified, ask the user which to use.
