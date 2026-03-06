---
name: grafana-investigate-alert
description: Use when the user asks about Grafana alerts — why an alert is firing, what it means, or its scope and impact. Trigger on phrases like "investigate alert", "why is this alert firing", "grafana alert", "alert firing", or when users mention specific alert names.
allowed-tools: [grafanactl, Bash]
---

# Grafana Alert Investigator

Investigate Grafana alerts by analyzing state, querying datasources, and identifying next steps. Be concise and direct - these are experienced operators who need actionable information, not hand-holding.

## Core Principles

1. Stop early for non-actionable scenarios (recording rules, healthy inactive alerts)
2. Be concise - no fluff, no excessive formatting, no obvious advice
3. Trust the user's expertise - no timelines, no patronizing suggestions
4. Focus on actionable information

## Prerequisites

User needs grafanactl installed with configured context and appropriate permissions.

## Investigation Workflow

### Step 1: Verify Context and Locate Alert

Check context if needed (`grafanactl config view`). If multiple contexts exist and none specified, ask which to use.

### Step 2: Get Alert Details and Check for Early Exit

Fetch the alert, by listing all alerts and filtering by name 
```bash
grafanactl alert rules list -o json | jq -r '.[] | .rules[]? | select(.name == "CertManagerCertExpirySoon")'
```

Filter by name, state, cluster/environment as relevant. If multiple matches, list them and ask which to investigate.
Inform the user which context you're using.

Check the `type` field:
- If `type: recording`: This is a recording rule, not an alerting rule. Report: "This is a recording rule (pre-calculates metrics), not an alerting rule. It doesn't fire alerts. Current state: [state]. Want details on what it's recording?" Stop here unless they ask for more.

Check the `state` field:
- If `state: inactive` AND the alert's query looks healthy: Report: "Alert is inactive. [Brief what it monitors]. Health: [health]. Last evaluated: [time]. Want to see historical trends?" Stop here unless they ask for more.
- If `state: firing` or `state: pending`: Continue with full investigation below.

### Step 3: Full Investigation (Firing/Pending Alerts Only)

You should use the datasourceUID from the alert when you can.

Query the datasource. Use -o json to get the data for yourself. Use with a graph visualization for showing a summary to the user:

```bash
# Prometheus
grafanactl query -d <datasource-uid> -e '<query>' --start now-1h --end now --step 1m -o json
grafanactl query -d <datasource-uid> -e '<query>' --start now-1h --end now --step 1m -o graph

# Loki
grafanactl query -d <datasource-uid> -e '<query>' --start now-1h --end now -o json
grafanactl query -d <datasource-uid> -e '<query>' --start now-1h --end now -o graph
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

Recommend incident creation if there's customer impact.

List specific next actions - queries to run, deployments to check, metrics to examine. If there are queries for logs or metrics you can run, then ask the user if they want you to run them. If infrastructure changes are a suspected cause, suggest to the user that you could investigate any infra-as-code repos, if they point you to them. 

If the next suggested actions include looking at logs in any way, use grafanactl to do it.

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

- If grafanactl fails, explain the error
- If no alerts match, show similar names and ask for clarification
- If datasource queries fail, note it and move on
- Multiple alerts with same name: list them all with UIDs and states, ask which to investigate

## Tips

- Graph visualization is critical for understanding trends
- Compare current values to baselines when relevant
- Check labels and annotations for environment/context
- Follow runbooks when available
- Err toward recommending incident creation when customer impact is unclear
