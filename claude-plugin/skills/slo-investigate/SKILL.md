---
name: slo-investigate
description: Use when a specific SLO is breaching or alerting and the user needs to understand why — root cause analysis, dimensional breakdown, alert rule correlation, runbook access. Trigger on phrases like "investigate SLO", "why is my SLO breaching", "SLO error budget burning", "SLO alerting". For SLO status overview use slo-check-status. For creating or modifying SLOs use slo-manage. For optimization suggestions use slo-optimize.
allowed-tools: [grafanactl, Bash]
---

# SLO Investigator

Deep-dive investigation of breaching SLOs: dimensional breakdown, alert correlation, runbook access. For experienced operators — no hand-holding.

## Core Principles

1. Use grafanactl commands — do not call Grafana APIs directly (no curl, no HTTP libraries)
2. Trust the user's expertise — skip obvious context, get to the root cause
3. Use `-o json` for agent processing, default format for user display; show graphs for time-series data
4. Errors collected at the end — do not interleave error handling in workflow steps
5. Use `--from`/`--to` for all time-range commands (never `--start`/`--end`)

## Investigation Workflow

### Step 1: Retrieve SLO Definition

```bash
grafanactl slo definitions get <UUID> -o json
```

Extract from the JSON response:
- `.metadata.name` — SLO name
- `.spec.query.type` — query type: `ratio`, `freeform`, or `threshold`
- For ratio: `.spec.query.ratio.successMetric`, `.spec.query.ratio.totalMetric`, `.spec.query.ratio.groupByLabels[]`
- For freeform: `.spec.query.freeform.query`
- `.spec.objectives[0].value` — objective (0–1), `.spec.objectives[0].window` — window
- `.spec.destinationDatasource.uid` — Prometheus datasource UID
- `.spec.alerting.fastBurn.annotations`, `.spec.alerting.slowBurn.annotations` — runbook/dashboard URLs
- `.metadata.annotations` — additional runbook/dashboard references

If no UUID is given, list SLOs and ask which to investigate:
```bash
grafanactl slo definitions list
```

### Step 2: Check Status with Wide Output

```bash
grafanactl slo definitions status <UUID> -o wide
```

This shows SLI, ERROR_BUDGET, BURN_RATE, SLI_1H, SLI_1D, and STATUS.

**Early exit — OK status:** If STATUS is OK, report health metrics and stop:
```
SLO: <name> — Status: OK
SLI: <value>  |  Error budget remaining: <budget>%  |  Burn rate: <rate>x
1h SLI: <sli_1h>  |  1d SLI: <sli_1d>
No action needed.
```

**Early exit — NODATA status:** If STATUS is NODATA, branch to NODATA diagnosis:
```
SLO: <name> — Status: NODATA
Recording rule metrics unavailable. Likely causes:
- Destination datasource misconfigured (check .spec.destinationDatasource.uid)
- Grafana recording rules not yet evaluated (can take 1–2 minutes after creation)
- Prometheus federation/remote write issue

Check: grafanactl datasources list --type prometheus
Then verify the destination datasource UID matches what the SLO expects.
```

**Lifecycle states:** If status is Creating/Updating/Deleting/Error, report that the SLO is in a transient state and investigate the Grafana backend.

### Step 3: Render Timeline

```bash
grafanactl slo definitions timeline <UUID> --from now-1h --to now
```

For wider trends:
```bash
grafanactl slo definitions timeline <UUID> --from now-24h --to now
```

Show the graph output (default). Use it to identify when breaching started and how severe it is.

### Step 4: Dimensional Breakdown

Resolve the datasource UID. If `.spec.destinationDatasource.uid` is set, use it. Otherwise auto-discover:
```bash
grafanactl datasources list --type prometheus
```

**For ratio queries** — extract success/total metric selectors and groupByLabels, then query dimensional breakdown:

```bash
# Success rate by dimension (e.g., cluster, status_code, endpoint)
grafanactl query -d <datasource-uid> \
  -e 'sum by (<groupByLabel>) (rate(<successMetric>[5m])) / sum by (<groupByLabel>) (rate(<totalMetric>[5m]))' \
  --from now-1h --to now --step 1m

# Error rate by dimension to spot the bad actor
grafanactl query -d <datasource-uid> \
  -e 'sum by (<groupByLabel>) (rate(<totalMetric>[5m])) - sum by (<groupByLabel>) (rate(<successMetric>[5m]))' \
  --from now-1h --to now --step 1m
```

If `groupByLabels` is empty, try common dimensions: `cluster`, `namespace`, `service`, `status_code`, `endpoint`.

**For freeform queries** — use the raw PromQL expression and add `by (<label>)` grouping:

```bash
grafanactl query -d <datasource-uid> \
  -e '<freeform_expression> by (cluster)' \
  --from now-1h --to now --step 1m

# Also try other likely breakdown dimensions
grafanactl query -d <datasource-uid> \
  -e '<freeform_expression> by (namespace)' \
  --from now-1h --to now --step 1m
```

Use graph output to display dimensional trends visually. Use `-o json` to extract exact values for the report.

### Step 5: Search for Related Alert Rules

```bash
grafanactl alert rules list -o json | jq '[.[] | .rules[]? | select(.name | test("<slo-name>"; "i"))]'
```

Also try searching by UUID fragment if the name-based search returns no results:
```bash
grafanactl alert rules list -o json | jq '[.[] | .rules[]? | select(.labels.slo_uuid == "<UUID>" or (.name | test("<slo-name>"; "i")))]'
```

Extract for each matching rule: name, state (firing/pending/inactive), labels, and annotations.

### Step 6: Extract Runbook and Dashboard URLs

Collect URLs from:
- `.spec.alerting.fastBurn.annotations.runbook_url`
- `.spec.alerting.fastBurn.annotations.dashboard_url`
- `.spec.alerting.slowBurn.annotations.runbook_url`
- `.spec.alerting.slowBurn.annotations.dashboard_url`
- `.metadata.annotations.*`

If a GitHub URL is found in runbook annotations and `gh` is available:
```bash
# Convert GitHub web URL to API path and fetch content
gh api /repos/<owner>/<repo>/contents/<path> --jq '.content' | base64 --decode
```

For raw GitHub URLs (`raw.githubusercontent.com`), extract the content URL pattern and use `gh api` with the equivalent API endpoint.

## Output Format

After completing the investigation, present results in this structure:

```
SLO: <name>
Target: <objective>% over <window>  |  Status: BREACHING
SLI: <current>%  |  Error budget remaining: <budget>%  |  Burn rate: <rate>x
1h SLI: <sli_1h>%  |  1d SLI: <sli_1d>%

[Timeline graph — show default output]

Dimensional Breakdown:
  Worst dimension: <label>=<value> at <error_rate>% error rate
  [Additional dimensions ranked by error rate]

Related Alert Rules:
  - <rule_name>: <state> [labels: <key>=<value>]

Runbook: <url>
Dashboard: <url>

[If runbook fetched]: Key runbook steps:
  <relevant excerpt>

Next actions:
1. <most specific actionable step based on findings>
2. <follow-up investigation or escalation path>
3. <if budget near zero: suggest slo-optimize for objective review>
```

## Error Handling

- **grafanactl slo definitions get fails with 404**: SLO UUID not found. Run `grafanactl slo definitions list` and confirm the UUID.
- **grafanactl slo definitions status returns empty**: No status available — SLO may be newly created. Check if recording rules are running (STATUS may show NODATA).
- **grafanactl query fails with datasource error**: Datasource UID may be wrong. Run `grafanactl datasources list --type prometheus` to find the correct UID.
- **grafanactl query returns no data**: The SLO metrics may write to a separate datasource (check `.spec.destinationDatasource.uid`). Try both the destination datasource and the default Prometheus datasource.
- **alert rules list returns empty**: Alert rules may be in a different folder. Try without filters: `grafanactl alert rules list -o json | jq length` to confirm total count.
- **gh api fails**: If `gh` is not authenticated or unavailable, report the runbook URL directly and skip content fetching.
- **SLO has no groupByLabels (ratio query)**: Try common breakdown dimensions: `cluster`, `namespace`, `service`, `endpoint`, `status_code`. Report which ones return data.
- **Multiple SLOs with similar names**: When searching alert rules by name pattern, report all matches and their states — don't silently drop duplicates.
