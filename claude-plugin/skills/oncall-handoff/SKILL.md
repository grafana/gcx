---
name: oncall-handoff
description: Use when the user wants a handoff report at the end of an on-call shift — a summary of every alert that paged during their most recent Grafana OnCall shift. Trigger on phrases like "oncall handoff", "end of shift report", "what paged during my shift", "handoff report", "summarize my on-call shift", or "what did I get paged for".
allowed-tools: [gcx, Bash]
---

# OnCall Shift Handoff Report

Build a handoff report for the user's **most recent** Grafana OnCall shift: discover which schedule/team they were on call for, which integrations route alerts to that schedule, and every alert group that fired during the shift window.

## Core Principles

1. Use `gcx irm oncall` — do not call OnCall APIs directly (no curl, no HTTP libraries).
2. Prefer `-o json` and filter with a single `jq` expression. Do **not** write bash `for`/`while` loops or other complicated/unsafe shell.
3. The CLI emits structured diagnostics on stderr (`hint:`, `note:`, `warn:`). They are informational — keep going.
4. This is a read-only reporting flow. Never run action verbs (`acknowledge`, `resolve`, `silence`, `delete`).
5. Collect errors as you go and report them at the end — a single missing integration link should not abort the report.

## Prerequisites

- `gcx` configured with an active context targeting a Grafana stack with OnCall enabled. If not, use the **setup-gcx** skill first.
- The user's **work email**. Ask for it if it was not provided. (Shortcut: `gcx irm oncall users current -o json` returns the authenticated user — use it only if the user confirms the report is for themselves.)

> Note on command differences: this repo exposes OnCall under `gcx irm oncall`. Unlike some older `gcx oncall` builds, `schedules list`, `integrations list`, and `escalation-policies list` here have **no team/chain filter flags**, and `alert-groups list` has **no `--started-at`** flag. List broadly and filter client-side with `jq`.
>
> JSON shape: most list commands emit a **bare JSON array**, so iterate with `.[]`. The exceptions are `alert-groups list` and `alert-groups list-alerts`, which wrap rows in `{"items": [...]}` — iterate those with `.items[]`. The examples below already use the right form for each command.

## Phase 1 — Find the most recent shift (schedule + team)

### 1.1 Resolve the user PK

```bash
gcx irm oncall users list -o json | jq -r '.[] | select(.spec.email == "<email>") | .metadata.name'
```

`metadata.name` is the OnCall user PK (e.g. `U1234567890`). If empty, the email may differ from OnCall's record — list a few users (`gcx irm oncall users list`) and confirm with the user.

### 1.2 Enumerate schedules

There is no `--team-id` filter. List every schedule and keep the team association for later:

```bash
gcx irm oncall schedules list -o json | jq -r '.[] | "\(.metadata.name)\t\(.spec.name)\t\(.spec.team // "-")"'
```

### 1.3 Narrow to the user's schedules

Do **not** run `final-shifts` against every schedule — a real stack has 100+ and that is hundreds of calls. Instead, find the handful the user is actually rostered on. `shifts list` returns the rotation *rules*, each carrying `spec.rolling_users` (the people in rotation, as priority-ordered groups) and `spec.schedule`. Filter for the user PK from 1.1:

```bash
gcx irm oncall shifts list -o json \
  | jq -r --arg pk "<user-pk>" '.[] | select(((.spec.rolling_users // []) | flatten) | index($pk)) | .spec.schedule' | sort -u
```

This yields the candidate schedule IDs in one command. (`shifts list` can be large and take a few seconds — that is expected.)

> Fallback: rotations driven by an iCal feed or web overrides have no `rolling_users`, so a user covered only that way won't appear here. If the user expects a shift that this step misses, fall back to scanning the schedules from 1.2 directly with `final-shifts` (one command each — still no loops).

### 1.4 Resolve the user's shifts (last 30 days)

`final-shifts` resolves the rendered rotation for a schedule between two dates. Flags are `--start` / `--end` (format `YYYY-MM-DD`), **not** `--start-date` / `--end-date`. It returns historical windows, so compute the dates relative to today (today's date is available in context; e.g. start = today − 30 days, end = today).

For each **candidate** schedule from 1.3, fetch final shifts and keep only the rows matching the user's email:

```bash
gcx irm oncall schedules final-shifts <schedule-id> --start <YYYY-MM-DD> --end <YYYY-MM-DD> -o json \
  | jq -r '.[] | select(.user_email == "<email>") | "\(.shift_start)\t\(.shift_end)\t\(.user_email)"'
```

Run this once per candidate schedule (each as a separate command — no loops). Skip schedules that return no matching shifts; do not investigate them further.

> Requires a gcx build that queries `filter_events` with the correct `date` parameter (and a valid timezone). Older builds silently ignore the start date and return only current/future shifts — if `final-shifts` never returns anything before today on any schedule, the gcx binary predates that fix; update gcx.

### 1.5 Pick the most recent shift

Across all matched shifts, select the one with the latest `shift_start` that is at or before now (the current or just-ended shift). Record its `shift_start`, `shift_end`, the **schedule ID**, and that schedule's **team**. That is the shift this report covers.

## Phase 2 — Find integrations routing to that schedule

The goal: which integrations actually page the schedule you were on. The link is `integration → route → escalation chain → escalation policy (notify_schedule)`.

### 2.1 Escalation policies that notify the schedule

`escalation-policies list` lists all policies (no `--escalation-chain` filter). A policy that pages an on-call schedule sets `spec.notify_schedule` to the schedule PK. Collect the escalation chains those policies belong to:

```bash
gcx irm oncall escalation-policies list -o json \
  | jq -r '.[] | select((.spec.notify_schedule // empty | tostring) == "<schedule-id>") | .spec.escalation_chain' | sort -u
```

This yields the escalation chain IDs that route to your schedule.

### 2.2 Routes → integrations

A route binds an integration (`spec.alert_receive_channel`) to an escalation chain (`spec.escalation_chain`). Find the integrations whose routes use one of the chains from 2.1:

```bash
gcx irm oncall routes list -o json \
  | jq -r '.[] | select((.spec.escalation_chain // empty | tostring) == "<chain-id>") | .spec.alert_receive_channel' | sort -u
```

The result is the set of **integration IDs** that page your schedule. Resolve their names for the report:

```bash
gcx irm oncall integrations list -o json \
  | jq -r '.[] | "\(.metadata.name)\t\(.spec.verbal_name)\t\(.spec.integration)"'
```

> Fallback (coarser): if the route/chain cross-reference comes up empty (e.g. policies reference the schedule indirectly), filter alert groups by the schedule's **team** instead of by integration — use `--team <team-id>` in Phase 3. Note this approximation in the report.

## Phase 3 — Alert groups during the shift

`alert-groups list` accepts `--integration <PK>` (repeatable / comma-separated) but has **no `--started-at`**. Two adjustments matter:

- The default filter **excludes resolved** groups. A past shift's alerts are usually resolved, so pass an explicit `--state firing,acknowledged,resolved,silenced`.
- Use `--max-age` to bound the lookback to cover the shift (set it to roughly `now − shift_start`, e.g. `36h`), then filter precisely client-side by the shift window.

```bash
gcx irm oncall alert-groups list \
  --integration <id1>,<id2> \
  --state firing,acknowledged,resolved,silenced \
  --max-age <duration covering the shift, e.g. 36h> \
  --limit 0 \
  -o json \
  | jq -r --arg start "<shift_start>" --arg end "<shift_end>" \
      '.items[] | select(.metadata.creationTimestamp >= $start and .metadata.creationTimestamp <= $end)
       | "\(.metadata.name)\t\(.status.state)\t\(.status.severity // "-")\t\(.status.title)"'
```

(`--limit 0` lifts the default 50-row cap so the window isn't truncated; the CLI still applies an internal safety cap.) Timestamps are RFC3339, so lexicographic comparison in `jq` is correct.

For richer detail on a notable alert group, drill in:

```bash
gcx irm oncall alert-groups get <id>
```

This populates `status.links.*` (alert rule, dashboard, SLO pivots) and `status.timestamps.*`. For repeated fires within a group, use `gcx irm oncall alert-groups list-alerts <id>`.

## Output: the handoff report

Produce a concise Markdown report:

```
# On-Call Handoff — <user email>

**Shift:** <shift_start> → <shift_end>
**Schedule:** <schedule name> (<schedule-id>)  •  **Team:** <team>
**Integrations covered:** <names/ids>

## Alerts during shift (<N> total)

| Started | State | Severity | Title | Alert Group |
|---------|-------|----------|-------|-------------|
| ...     | ...   | ...      | ...   | <id>        |

## Notable / still-open
- <firing/acknowledged groups, with status.links.alert.rule.url where present>

## Handoff notes
- <anything still firing, recurring fires, or follow-ups for the next on-call>
```

If there were **no** alerts during the shift, say so plainly — that is a valid (good) outcome.

## Error Handling

Collect non-fatal errors and report them at the end of the workflow.

| Situation | Action |
|-----------|--------|
| Email matches no user | List users (`gcx irm oncall users list`) and confirm the OnCall email with the user. |
| No shifts found for the user in 30 days | Report it. Optionally widen the `--start` date and retry. |
| No escalation policy references the schedule | Use the team-level fallback in Phase 2 (`--team <team-id>` in Phase 3) and note the approximation. |
| `routes list` / cross-reference empty | Fall back to `--team` filtering; note it. |
| `alert-groups list` returns nothing | Confirm `--state` includes `resolved` and `--max-age` covers the shift start. |
| Auth / connectivity errors | Check the context: `gcx config view`. Use the **setup-gcx** skill if needed. |

## Related Skills

- **oncall-triage** — what is paging *right now* (active alert groups), plus drill-in and action verbs.
- **investigate-alert** — rule-side root cause once you have `status.links.alert.rule.uid` from an alert group.
- **post-incident-review** — for a full PIR if a shift alert escalated into an incident.
