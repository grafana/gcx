---
name: slo-optimize
description: |
  Use when the user wants to analyze SLO performance trends and receive improvement recommendations —
  objective tuning, alerting sensitivity review, label visibility, or window adjustments.
  Trigger on phrases like "optimize my SLO", "SLO improvement suggestions", "tune my SLO",
  "SLO performance analysis", or "should I change my SLO objective".
  For SLO status overview use slo-check-status.
  For investigating breaching SLOs use slo-investigate.
  For creating or modifying SLO definitions use slo-manage.
allowed-tools: [gcx, Bash]
---

# SLO Optimizer

Analyze SLO timeline trends, compute statistics over the past 28 days, and generate advisory
recommendations backed by real metric values. Never modify SLO definitions directly — route to
slo-manage when the user wants to apply a recommendation.

## Core Principles

1. Use gcx commands exclusively — do not call Grafana APIs directly.
2. Trust the user's expertise — skip explanations of what SLOs or burn rates are.
3. Use `-o json` for agent processing of structured output; default format for user display.
4. Show graph output for timeline data so the user can see the trend visually.
5. Every recommendation MUST include supporting data (current values, projected values, or
   historical comparisons). No generic advice without numbers.
6. This skill is advisory only. Route to slo-manage for any changes the user wants to apply.

## Prerequisites

gcx configured with a context pointing to the target Grafana instance.

If the user does not supply a UUID, list available SLOs first:

```bash
gcx slo definitions list
```

Ask the user which SLO to analyze if the target is ambiguous.

## Optimization Workflow

### Step 1: Retrieve SLO Definition

```bash
gcx slo definitions get <UUID> -o json
```

Extract and note:
- `spec.name` — display name
- `spec.objectives[0].value` — current objective (e.g., 0.999)
- `spec.objectives[0].window` — compliance window (e.g., 28d)
- `spec.query.type` — ratio | freeform | threshold
- `spec.query.ratio.groupByLabels` — dimensional labels (may be empty)
- `spec.alerting` — fastBurn / slowBurn configuration (may be absent)
- `spec.destinationDatasource.uid` — datasource UID for metric queries

### Step 2: Fetch 28-Day Timeline

```bash
# Default graph output for user display
gcx slo definitions timeline <UUID> --from now-28d --to now

# JSON output for statistical analysis
gcx slo definitions timeline <UUID> --from now-28d --to now -o json
```

Parse the JSON output to extract SLI values across the time series. Compute:
- `mean_sli` — average SLI over the 28-day window
- `min_sli` — lowest observed SLI point
- `max_sli` — highest observed SLI point
- `std_dev` — variability indicator

If timeline returns no data (NODATA), note it and skip to Step 3 for current status.

### Step 3: Get Current Status (Wide Format)

```bash
gcx slo definitions status <UUID> -o wide
```

Extract from the wide output:
- Current SLI value
- Error budget remaining (%)
- Burn rate (current)
- SLI_1H and SLI_1D snapshots
- Status: OK | BREACHING | NODATA

### Step 4: Query Raw SLI Metrics (When Timeline Is Insufficient)

When timeline data is sparse (< 7 days of points) or all NODATA, query raw metrics directly
using the datasource UID from Step 1:

```bash
# SLI window metric (primary trend signal)
gcx metrics query <datasource-uid> \
  'grafana_slo_sli_window{slo_uuid="<UUID>"}' \
  --from now-28d --to now --step 6h

# Success and total rate for ratio SLOs
gcx metrics query <datasource-uid> \
  'grafana_slo_success_rate_5m{slo_uuid="<UUID>"}' \
  --from now-28d --to now --step 6h

gcx metrics query <datasource-uid> \
  'grafana_slo_total_rate_5m{slo_uuid="<UUID>"}' \
  --from now-28d --to now --step 6h
```

If the datasource UID is not in the definition, resolve it:

```bash
gcx datasources list --type prometheus
```

### Step 5: Analyze Trends

Classify the pattern using the timeline data from Steps 2 and 4:

**Sustained decline** — SLI trending downward for 7 or more consecutive days. Compute the
slope over the last 7 days vs. the preceding 7 days to confirm direction.
- Recommendation trigger: investigate underlying service degradation; a window adjustment will
  not fix a declining service.

**Periodic dips** — SLI drops recur at regular intervals (e.g., every weekend, every night).
Look for temporal correlation in the min points.
- Recommendation trigger: window adjustment (e.g., 7d → 28d smooths weekend traffic spikes)
  or objective reduction if the dips are expected.

**Sudden drops** — Step-change in SLI at a specific timestamp (deployment, config change).
Identify the onset timestamp and estimate error budget consumed by the event.
- Recommendation trigger: check alerting is configured; if budget consumed > 20% by a single
  event, consider tighter fastBurn thresholds.

**Budget exhaustion rate** — Project when the error budget will reach 0 based on the current
burn rate from Step 3. Formula:
  `days_until_exhausted = budget_remaining_pct / (burn_rate * 100 / window_days)`
- Recommendation trigger: if < 7 days remain, flag as urgent; route to slo-investigate.

### Step 6: Generate Advisory Recommendations

Produce numbered recommendations. Each recommendation requires:
1. A specific change (what to do)
2. Supporting data (why — current value vs. proposed value)
3. Expected outcome

**Objective tuning**

If `mean_sli < objective - 0.005` (more than 0.5 pp below the objective):
- Suggest lowering the objective to `floor(mean_sli * 1000) / 1000` (rounded down to 3 dp).
- Include: current objective, observed mean SLI, proposed objective.
- Rationale: the SLO is chronically breaching due to an unrealistic target.

If `mean_sli > objective + 0.010` (more than 1 pp above the objective):
- Suggest tightening the objective toward `mean_sli - 0.005`.
- Include: current objective, observed mean SLI, proposed objective.
- Rationale: the SLO is trivially satisfied; tighten to reflect achievable performance.

**groupByLabels addition (ratio query type only)**

If `spec.query.ratio.groupByLabels` is empty or absent:
- Recommend adding dimensional labels such as `cluster`, `service`, `endpoint`, or
  `status_code` depending on what labels exist in the underlying metric series.
- Rationale: without groupByLabels, all dimensions are collapsed — the SLO cannot identify
  which dimension is causing a breach.

**Alerting configuration**

If `spec.alerting` is absent or empty:
- Recommend configuring fastBurn (page) and slowBurn (ticket) alerts.
- Example thresholds: fastBurn `burnRateThreshold: 14.4` over 1h (consumes 2% budget/hour),
  slowBurn `burnRateThreshold: 1` over 6h.

If alerting is configured and current burn rate (from Step 3) has been above 2x for the past
7 days (compare burn rate from status with recent timeline values):
- Recommend reviewing alerting thresholds — existing alerts may not be firing despite sustained
  budget drain.
- Include: current burn rate, alert threshold from definition, observed duration above 2x.

**Window adjustment**

If the SLO window is 7d and periodic dips are detected (weekend pattern):
- Recommend switching to 28d to smooth the variability.
- Include: current window, dip frequency, estimated improvement in budget consumption.

If the SLO window is 28d or 30d and `mean_sli` is very stable (std_dev < 0.001):
- Note the window is appropriate; no change needed.

### Step 7: Present Recommendations and Route to slo-manage

Present all recommendations as advisory text. Do not apply any changes.

After presenting recommendations, ask:
> "Would you like me to apply any of these recommendations? If so, I'll switch to slo-manage
> to pull the current definition and implement the changes with a dry-run first."

If the user confirms, invoke the slo-manage skill to handle the update workflow.

## Output Format

```
SLO: <name>
UUID: <uuid>
Objective: <value> over <window>
Analysis period: now-28d to now

SLI Statistics (28d):
  Mean: <value>   Min: <value>   Max: <value>
  Std dev: <value>

Current Status:
  SLI: <value>   Budget remaining: <pct>%   Burn rate: <value>x
  SLI (1h): <value>   SLI (1d): <value>

[28-day timeline graph]

Trend classification: <Sustained decline | Periodic dips | Sudden drops | Stable>
<One sentence describing the dominant pattern with supporting data>

Advisory Recommendations:

1. <Recommendation title>
   Current: <value>
   Proposed: <value>
   Why: <rationale with numbers>

2. <Recommendation title>
   ...

[If no recommendations apply:]
No objective or alerting changes recommended. The SLO configuration appears well-calibrated
for the observed performance over the past 28 days.

---
To apply a recommendation: slo-manage will pull the definition and apply the change with
a dry-run. Confirm which recommendation(s) you want to apply.
```

## Error Handling

Collect errors; report them at the end of the analysis, not interleaved with findings.

- **`gcx slo definitions get` fails (not found)**: Confirm the UUID and context.
  Run `gcx slo definitions list` to show available SLOs.

- **Timeline returns NODATA**: Recording rule metrics may not be populating. Check the
  destination datasource configuration. Proceed with raw metric queries in Step 4. If raw
  metrics also return NODATA, report the data gap and recommend verifying that the SLO
  recording rules are evaluating correctly.

- **Datasource UID not in definition**: Run `gcx datasources list --type prometheus`
  and present the list to the user. Do not block the analysis — use the remaining timeline
  data from Step 2.

- **Timeline data < 7 days of points**: The SLO may be newly created. Note the limited
  analysis window, proceed with available data, and suppress trend classifications that
  require 7+ days of data.

- **Status returns BREACHING**: Note the breach in the output. Include budget exhaustion
  rate in the recommendations. Route to slo-investigate for deeper root cause analysis if
  the user wants to understand why the SLO is breaching (not just optimize it).

- **gcx command not found or auth error**: Check `gcx config view` to verify
  the active context and credentials.
