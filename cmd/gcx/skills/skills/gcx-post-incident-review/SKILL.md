---
name: gcx-post-incident-review
description: (Experimental) Generate a structured post-incident review using gcx incidents, oncall, slo, metrics, logs, and dashboard resources.
user-invocable: true
disable-model-invocation: false
allowed-tools: Bash, Read, Write, Grep, Agent, AskUserQuestion
---

# Post-Incident Review (PIR)

Create a complete PIR for one IRM incident using only commands available in this repo.

## Rules

1. Discover and verify data first, then write the report.
2. Use `-o json` for machine processing.
3. Use client-side filtering (time/title/labels) when list commands lack server-side filters.
4. Keep timestamps explicit and UTC in the final timeline.

## Step 1: Identify Incident

Ask user for one of:
- Incident ID
- Full incident URL
- "most recently resolved incident"

Commands:

```bash
gcx incidents list --limit 200 -o json
gcx incidents get <incident-id> -o json
```

If user requests "most recent resolved", select the latest resolved incident from list output.

## Step 2: Fetch Core Incident Context

```bash
gcx incidents get <incident-id> -o json
gcx incidents severities list -o json
gcx incidents activity list <incident-id> --limit 200 -o json
```

Extract at minimum:
- `id`, `title`, `status`, `severity`
- `incidentStart` (or earliest equivalent start field)
- `createdTime`, `modifiedTime`/resolution time
- labels/team/service/env
- notable activity entries (root-cause clues, resolution notes, links)

## Step 3: Correlate Alert Groups

```bash
gcx oncall alert-groups list --max-age 30d -o json
gcx oncall alert-groups get <alert-group-id> -o json
```

Filter alert groups to the incident window (plus pre-window buffer, e.g. 1h).
Correlate by:
- time overlap
- title/keyword overlap
- shared labels/service names

For key groups, fetch details with `get`.

## Step 4: SLO Impact

```bash
gcx slo definitions list -o json
gcx slo definitions status <slo-id> -o json
gcx slo reports list -o json
```

Identify impacted SLOs by team/service naming or labels.
Capture burn/error-budget context if present.

## Step 5: Telemetry Evidence

Find datasource IDs first:

```bash
gcx datasources list -o json
```

Then gather evidence around incident window:

```bash
gcx metrics query <prom-uid> '<promql>' --from <start> --to <end> -o json
gcx logs query <loki-uid> '<logql>' --from <start> --to <end> -o json
```

Prefer concrete queries tied to incident service:
- request/error rate trend
- latency trend (p95/p99)
- top error logs matching activity notes

## Step 6: Dashboard Context

```bash
gcx resources get dashboards -o json
```

Find relevant dashboards by title/labels/service keywords.
Resolve stack URL:

```bash
gcx config current-context
gcx config view -o json
```

Build deep links:
- Dashboard: `<stack_url>/d/<uid>?from=<from>&to=<to>`
- Incident: `<stack_url>/a/grafana-irm-app/incidents/<id>`
- Alert group: `<stack_url>/a/grafana-oncall-app/alert-groups/<id>`

## Step 7: Write PIR

Save file as: `PIR-<YYYY-MM-DD>-<slug>.md`

Use this structure:

```markdown
# <YYYY-MM-DD> <Incident Title>

Date: <YYYY-MM-DD>
Status: draft
Incident: [<id>](<incident-link>)

## Summary
<2-4 sentence executive summary>

## Impact
- <who/what was affected>
- <duration>
- <SLO impact>

## Root Cause
- <primary root cause>
- <contributing factors>

## Trigger
- <deploy/config/event trigger if identified>

## Detection
- <first alert group time>
- <incident declaration time>
- <detection gap>

## Resolution
- <what was done>
- <what confirmed recovery>

## Action Items
| Action Item | Type | Owner | Status |
|---|---|---|---|
| ... | ... | ... | ... |

## Timeline (UTC)
<timestamp> - <event>
<timestamp> - <event>

## Supporting Evidence
- Dashboard: [<title>](<url>)
- Alert groups: [<name>](<url>)
- Key metrics/log queries used
```

## Failure Handling

- If no incident activity is available: still produce PIR with explicit "unknown" sections.
- If no correlated alert groups: note "no correlated alert groups found in window".
- If telemetry queries fail: include command + error and continue.
- If multiple possible root causes exist: present top hypotheses with confidence levels.
