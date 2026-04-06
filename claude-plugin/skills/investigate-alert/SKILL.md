---
name: investigate-alert
description: Investigate Grafana alerts to determine why they are firing, their scope, and impact. Use when the user asks about a specific alert, wants to understand alert behavior, or needs to diagnose why an alert is in a firing or pending state.
---

# Grafana Alert Investigator

Investigate Grafana alerts by analyzing state, querying datasources, and identifying next steps. Be concise and direct - these are experienced operators who need actionable information, not hand-holding.

## Core Principles

1. Stop early for non-actionable scenarios (recording rules, healthy inactive alerts)
2. Be concise - no fluff, no excessive formatting, no obvious advice
3. Trust the user's expertise - no timelines, no patronizing suggestions
4. Focus on actionable information

## Prerequisites

User needs gcx installed with configured context and appropriate permissions. If gcx is not configured, use the setup-gcx skill first.

## Investigation Workflow

### Step 1: Verify Context and Locate Alert

Check context if needed (`gcx config view`). If multiple contexts exist and none specified, ask which to use.

### Step 2: Get Alert Details and Check for Early Exit

Fetch the alert by listing all alerts and filtering by name. Replace `<AlertName>` with the actual alert name:
```bash
gcx alert rules list -o json | jq -r '.[] | .rules[]? | select(.name == "<AlertName>")'
```

Server-side filters (use instead of downloading all rules and filtering with jq):
- `--state firing|pending|inactive` — filter by rule state
- `--group <name>` — filter by group name
- `--folder <uid>` — filter by folder UID

Filter by name, state, cluster/environment as relevant. If multiple matches, list them and ask which to investigate.
Inform the user which context you're using.

Check the `type` field:
- If `type: recording`: This is a recording rule, not an alerting rule. Report: "This is a recording rule (pre-calculates metrics), not an alerting rule. It doesn't fire alerts. Current state: [state]. Want details on what it's recording?" Stop here unless they ask for more.

Check the `state` field:
- If `state: inactive` AND the alert's query looks healthy: Report: "Alert is inactive. [Brief what it monitors]. Health: [health]. Last evaluated: [time]. Want to see historical trends?" Stop here unless they ask for more.
- If `state: firing` or `state: pending`: Continue with full investigation below.

### Step 3: Full Investigation (Firing/Pending Alerts Only)

You should use the datasourceUID from the alert when you can.

If you need to query a different datasource (e.g., Loki for log correlation), resolve its UID first:
```bash
gcx datasources list --type loki
```
Annotation URLs often reference datasources by name — always resolve to UID before querying.

Query the datasource. Use -o json to get the data for yourself. Use with a graph visualization for showing a summary to the user:

```bash
# Prometheus
gcx metrics query <datasource-uid> '<query>' --from now-1h --to now --step 1m -o json
gcx metrics query <datasource-uid> '<query>' --from now-1h --to now --step 1m -o graph

# Loki
gcx logs query <datasource-uid> '<query>' --from now-1h --to now -o json
gcx logs query <datasource-uid> '<query>' --from now-1h --to now -o graph
```

Analyze the results: What's the current value? Spike or gradual? When did it start?

### Step 4: Surface Resources and Provide Analysis

Extract from annotations:
- Runbook URLs (if the URL is a GitHub URL and `gh` is available, fetch with `gh api`)
- Dashboard links
- Descriptions

Provide concise analysis:
- Where: cluster/environment from labels
- What: affected system/service
- Trend: new spike vs ongoing
- Likely causes: code changes, infrastructure, resource exhaustion
- Customer impact: if relevant

Based on the error class, suggest follow-up queries to the user:
- **Connection errors**: Check endpoint availability (`up{job="..."}`) and pod restart counts
- **Latency spikes**: Check upstream service latency, database query duration metrics
- **Error rate increase**: Break down by endpoint/handler, correlate with recent deployments
- **Resource exhaustion**: Check container CPU/memory metrics and node capacity

Recommend incident creation if there's customer impact.

List specific next actions - queries to run, deployments to check, metrics to examine. If there are queries for logs or metrics you can run, then ask the user if they want you to run them. If infrastructure changes are a suspected cause, suggest to the user that you could investigate any infra-as-code repos, if they point you to them.

If the next suggested actions include looking at logs in any way, use gcx to do it.

## Output Format

For recording rules or healthy inactive alerts (early exit):
```
This is a [recording rule / inactive alert]. [One sentence what it monitors]. State: [state]. Health: [health].

Want to see more details?
```

For firing/pending alerts (full investigation):
```
Alert: <name>
State: firing [in <cluster/env>]
Monitors: <brief what it checks>

[Show graph visualization]

Current value: <value>
Trend: <spike/gradual/sustained>

Likely causes:
- <cause 1>
- <cause 2>

Impact: <who/what affected>

Runbook: <link>
Dashboard: <link>

Next actions:
- <action 1>
- <action 2>

[If customer impact:] Recommend creating an incident - <why>.
```

Use minimal formatting. Avoid excessive bold text. No timelines like "within 24 hours". Trust the user to prioritize.

## Error Handling

- If gcx fails, explain the error
- If no alerts match, show similar names and ask for clarification
- If datasource queries fail, note it and move on
- Multiple alerts with same name: list them all with UIDs and states, ask which to investigate

## Tips

- Graph visualization is critical for understanding trends
- Compare current values to baselines when relevant
- Check labels and annotations for environment/context
- Follow runbooks when available
- Err toward recommending incident creation when customer impact is unclear

## Reference

For alert JSON structure, query patterns by alert type, graph interpretation, and runbook fetching, see:
- [`references/alert-investigation-patterns.md`](references/alert-investigation-patterns.md)
