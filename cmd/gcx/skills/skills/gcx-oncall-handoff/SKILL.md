---
name: gcx-oncall-handoff
description: (Experimental) Generate an on-call handoff report for the most recent completed shift using gcx OnCall and IRM data.
user-invocable: true
disable-model-invocation: false
allowed-tools: Bash, Read, Write, Grep, Agent, AskUserQuestion
---

# On-Call Handoff (gcx)

Build a concise shift handoff for the user's most recent completed shift.
Use only commands that exist in this repo.

## Rules

1. Use gcx commands only for Grafana data.
2. Keep alert groups and incidents as separate concepts.
3. Prefer `-o json` + client-side filtering over assumed server-side filters.
4. When independent fetches are needed, run them in parallel.

## Step 1: Resolve User + Stack URL

If user email is unknown, ask once.

```bash
gcx config current-context
gcx config view -o json
```

Extract stack URL from the active context:
- `.contexts[<current-context>].grafana.server`

Find user in OnCall:

```bash
gcx oncall users list -o json
gcx oncall users get <user-id> -o json
```

## Step 2: Find Most Recent Completed Shift

List schedules, then fetch final shifts for the last 30 days.
Use schedule calls in parallel.

```bash
gcx oncall schedules list -o json
gcx oncall schedules final-shifts <schedule-id> --start <YYYY-MM-DD> --end <YYYY-MM-DD> -o json
```

From all shifts, keep only shifts assigned to the target user.
Pick the most recent completed shift (`end < now`).

If none found, report that no completed shifts were found in the selected window.

## Step 3: Map Integrations Related to Shift Schedules

Fetch integrations and their details in parallel:

```bash
gcx oncall integrations list -o json
gcx oncall integrations get <integration-id> -o json
```

Fetch escalation policy data:

```bash
gcx oncall escalation-policies list -o json
gcx oncall escalation-policies get <policy-id> -o json
```

Correlate integrations/policies to the shift schedule(s) where possible
(e.g. policies that reference schedule IDs, or integrations linked to those chains).

## Step 4: Collect Alert Groups + Incidents in Shift Window

Get alert groups and incidents, then filter client-side to the shift time range.

```bash
gcx oncall alert-groups list --max-age 30d -o json
gcx incidents list --limit 200 -o json
```

For incidents in-window, fetch details as needed:

```bash
gcx incidents get <incident-id> -o json
```

Correlate incidents with alert groups by:
- Time overlap (or within ~30m around incident start)
- Title/keyword overlap
- Shared service/team labels

## Step 5: Optional Team/Schedule Enrichment

```bash
gcx oncall teams list -o json
gcx oncall teams get <team-id> -o json
gcx oncall schedules get <schedule-id> -o json
```

Use these for friendly names and links in report output.

## Deep Link Patterns

Use `<stack_url>` from config:
- User: `<stack_url>/a/grafana-oncall-app/users/<id>`
- Team: `<stack_url>/org/teams/edit/<numeric_id>`
- Schedule: `<stack_url>/a/grafana-oncall-app/schedules/<id>`
- Integration: `<stack_url>/a/grafana-oncall-app/integrations/<id>`
- Escalation chain: `<stack_url>/a/grafana-oncall-app/escalation-chains/<id>`
- Alert group: `<stack_url>/a/grafana-oncall-app/alert-groups/<id>`
- Incident: `<stack_url>/a/grafana-irm-app/incidents/<id>`

## Output Format

```markdown
## On-Call Handoff Report

**User:** [<name>](<user-link>) (<email>)
**Shift:** <start> -> <end>
**Schedule:** [<schedule-name>](<schedule-link>)

### Shift Summary
- Total alert groups in shift window: <n>
- Open alert groups at handoff: <n>
- Incidents overlapping shift: <n>

### Incidents During Shift
- [<incident-title>](<incident-link>) — <status>, <severity>, started <time>
  - Related alert groups: [<alert-title>](<alert-link>), ... or "none identified"

### Major Alert Groups
- [<alert-title>](<alert-link>) — <status>, started <time>, integration <name>

### Ongoing/Open at Handoff
- [<alert-title>](<alert-link>) — <status> since <time>
- [<incident-title>](<incident-link>) — <status> since <time>

### Alert Volume by Integration
- [<integration-name>](<integration-link>) — total <n>, open <n>, resolved <n>

### Recommendations for Incoming On-Call
1. <highest-priority follow-up>
2. <next action>
3. <monitoring/check recommendation>
```

## Failure Handling

- If no matching user by email: ask for exact email or user ID.
- If no shifts found: report that and suggest widening date window.
- If no alert groups in window: still produce report with incidents + context.
- If incidents lookup fails: produce alert-only handoff and note the gap.
