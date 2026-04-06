---
name: slo-check-status
description: Use when the user asks about SLO health, wants an overview of all SLOs, or needs status of a specific SLO. Trigger on phrases like "how are my SLOs doing", "SLO status", "check my SLOs", "is my SLO healthy", "SLO budget", "SLO burn rate". For investigating breaching SLOs use slo-investigate. For optimization suggestions use slo-optimize. For creating or modifying SLO definitions use slo-manage.
allowed-tools: [gcx, Bash]
---

# SLO Status Checker

Check SLO health, budget consumption, and trends. Route to investigation or optimization as needed.

## Core Principles

1. Use `gcx` commands — do not call Grafana APIs directly
2. Trust the user's expertise — no hand-holding or excessive explanation
3. Use `-o json` for agent processing, default format for user display
4. Show graphs for time-series data (timeline commands default to graph output)

## Workflow

### Step 1: Overview or Specific SLO

**If the user asks about all SLOs (no specific UUID):**

```bash
gcx slo definitions list
```

This shows UUID, name, target objective, window, and status for all SLOs.

Then get the health summary:

```bash
gcx slo definitions status
```

This shows SLI, error budget, and health status for all SLOs in a table.

**If the user asks about a specific SLO (UUID or name provided):**

```bash
gcx slo definitions status <UUID> -o wide
```

The `-o wide` output includes additional columns: BURN_RATE, SLI_1H, and SLI_1D, which give a richer picture of recent performance.

### Step 2: Interpret Status Values

| Status | Meaning |
|--------|---------|
| OK | SLI >= objective. SLO is healthy. |
| BREACHING | SLI < objective. Error budget is being consumed. |
| NODATA | No Prometheus metrics from recording rules. |
| Creating | SLO provisioning in progress. |
| Updating | SLO update in progress. |
| Deleting | SLO deletion in progress. |
| Error | SLO in error state — check the SLO configuration. |

**NODATA handling:** Recording rule metrics may not be available if the destination datasource is misconfigured or recording rules are not evaluating. Note this to the user and suggest checking the destination datasource configuration.

### Step 3: Timeline (Conditional)

Show the timeline when:
- The user asks about trends or historical data
- Any SLO shows BREACHING status

**All SLOs timeline:**
```bash
gcx slo definitions timeline --from now-7d --to now
```

**Specific SLO timeline:**
```bash
gcx slo definitions timeline <UUID> --from now-7d --to now
```

Use `--since` as a shorthand when a single duration is more natural:
```bash
gcx slo definitions timeline <UUID> --since 7d
```

The timeline command renders a graph by default — this is the preferred output for users.

Adjust the time range based on the SLO window:
- 7d window SLO → use `--from now-7d --to now`
- 28d or 30d window SLO → use `--from now-28d --to now`

### Step 4: SLO Reports Status (Conditional)

When the user asks about SLO reports or wants combined SLO health from the reports subsystem:

```bash
gcx slo reports status
```

Or for a specific SLO:
```bash
gcx slo reports status <UUID>
```

### Step 5: Route to Investigation or Optimization

**BREACHING SLOs:** After showing the timeline, suggest slo-investigate:

> SLO `<name>` is BREACHING (SLI: <value>, objective: <value>). For a deep-dive investigation — raw metrics, dimensional breakdown, alert rules — use the **slo-investigate** skill.

**User asks about improvements:** Route to slo-optimize:

> For trend analysis, objective tuning recommendations, and alerting sensitivity review, use the **slo-optimize** skill.

## Output Format

**All-SLOs overview:**
```
SLOs: <total> total, <n> OK, <n> BREACHING, <n> NODATA

[Table from gcx slo definitions status]

[If any BREACHING: show timeline graph]
[If BREACHING: suggest slo-investigate for each BREACHING SLO]
```

**Specific SLO status:**
```
SLO: <name>
Status: <OK|BREACHING|NODATA>
SLI: <value> (objective: <value>, window: <value>)
Error budget remaining: <value>
Burn rate: <value>
SLI_1H: <value> | SLI_1D: <value>

[If trend requested or BREACHING: show timeline graph]
[Routing suggestions as applicable]
```

Use minimal formatting. Lead with the status and key metrics. No excessive bold text.

## Error Handling

Collect all errors and report at the end of the workflow — do not interrupt the workflow for non-fatal errors.

| Error | Action |
|-------|--------|
| `gcx slo definitions list` returns empty | Report "No SLOs found in this context." Check context with `gcx config view`. |
| `gcx slo definitions status` returns NODATA for all SLOs | Note that recording rule metrics are unavailable. Suggest checking the destination datasource configured on each SLO definition. |
| `gcx slo definitions status <UUID>` — UUID not found | List all SLOs to help the user identify the correct UUID. |
| `gcx slo definitions timeline` fails | Note the failure and continue. Timeline is supplementary. |
| `gcx slo reports status` fails | Note the failure. Reports status is optional context. |
| Auth errors | Check context configuration: `gcx config view`. Ensure the server URL and credentials are set. |
