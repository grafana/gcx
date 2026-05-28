---
name: oncall-triage
description: Use when the user is triaging what is actively paging in Grafana OnCall, or asks about active alert groups, acknowledging or silencing or resolving fires, on-call queue, or "what's paging right now". Trigger on phrases like "what's paging", "on-call alerts", "ack this", "silence the page", "what's firing in OnCall", "show me active pages", or any reference to OnCall alert groups. For root cause of why a Grafana alert rule is evaluating (rule-side, pre-routing) use investigate-alert. For schedules, integrations, or escalation chains use the gcx skill.
allowed-tools: [gcx, Bash]
---

# OnCall Alert-Group Triage

OnCall **alert groups** are post-routing aggregations of alert deliveries — they answer "what is paging right now?". Grafana **alert rules** (under `gcx alert rules`) answer "why is rule X evaluating to firing?". This skill covers the OnCall side and pivots out when needed.

## Core Principles

1. Use gcx — do not call OnCall APIs directly (no curl, no HTTP libraries).
2. The CLI emits structured diagnostics on stderr (`hint:`, `note:`, `warn:`; JSONL with `class` in agent mode). Pass them through.
3. The default `alert-groups list` filter excludes resolved + child groups. Surface `--all` only when the user asks about history.
4. Action verbs in agent mode require `--yes` when matched count > 1. Never pre-fill `--yes` without user confirmation.

## Prerequisites

gcx configured with an active context that targets a Grafana stack with OnCall enabled. If not, use the setup-gcx skill first.

## Triage Workflow

### Step 1: List Active Alert Groups

```bash
gcx irm oncall alert-groups list
```

Default filter: `state in {firing, acknowledged, silenced}` and `is_root=true` (matches the OnCall UI). See `--help` for the full flag set; common narrowers are `--state`, `--team <PK>`, `--integration <PK>`, `--mine`, `--max-age 24h`. Team / integration filters take PKs, not names — resolve first:

```bash
gcx irm oncall teams list -o json | jq -r '.[] | "\(.metadata.name): \(.spec.name)"' | grep -i "my team"
```

Escape hatches: `--all` (drops both defaults — returns resolved + child groups), `--include-child-groups`, `--state resolved`.

Default table: `ID TITLE SEVERITY STATE TEAM SUBJECT AGE`. `-o wide` adds `RULE` (Grafana rule URL) and `ALERTS` (group-wide count). Use `-o wide` when the user needs the rule pivot.

There is no `--title` / substring filter. To act on "all the kafka ones", fetch JSON, filter with jq, then loop the IDs through the action verb (or — if the kafka alerts all share an integration or team — bulk-by-filter with `--integration <PK>` / `--team <PK>` instead):

```bash
gcx irm oncall alert-groups list -o json | \
  jq -r '.items[] | select(.status.title | test("kafka"; "i")) | .metadata.name'
```

### Step 2: Drill Into a Group

```bash
gcx irm oncall alert-groups get <id>
```

One round trip already populates the rich `status.links.*` block — typical triage does NOT need `list-alerts` first.

Key paths under `status`:
- `title`, `severity`, `state` (`firing|acknowledged|resolved|silenced`), `summary`, `runbookURL`
- `subject.labels` — canonical commonLabels (whatever the rule grouped by; canonical only on `get`, best-effort on `list`)
- `timestamps.{started,acknowledged,resolved,silenced}`
- `links.alert.rule.{uid,url}` — Grafana alert rule pivot (most important)
- `links.alert.instance.{id,silenceURL}` — Alertmanager fingerprint
- `links.dashboard.{uid,url,panel.id,panel.url}`
- `links.slo.{uid,name}`
- `alertsCount`

Also: `spec.permalinks.web` (OnCall UI), `spec.team.{id,name}`, `spec.integration.{id,name,type}`.

`--include-raw` adds the unprocessed Alertmanager payload at `status.raw` — only when the user needs an unpromoted label or annotation.

### Step 3: Per-Alert Detail (when needed)

Only when the group has multiple firing instances and the user needs per-fire detail:

```bash
gcx irm oncall alert-groups list-alerts <id>
```

Default collapses by label set (Alertmanager fingerprint) — repeated fires of the same labeled instance fold into one row, `status.occurrences` reports the re-fire count. `--history` opts out (every delivery becomes a row, `occurrences: 1`). `--slim` skips the per-alert fetch for counting/sorting. `--include-raw` exposes the full payload. `--limit` default 100; the CLI warns when capped.

Per-alert key fields: `status.dimensions.labels` (per-fire discriminators — labels that differ from the group's `subject.labels`), `status.occurrences`, `status.links.*` (usually constant across siblings).

### Step 4: Pivot to Alert Rule / Dashboard / SLO

The `status.links.*` identifiers are cross-provider pivots:

| Source field | Next command |
|---|---|
| `status.links.alert.rule.uid` | `gcx alert instances list --rule <uid>` — all currently firing instances across clusters; hand off to **investigate-alert** for rule-side root cause |
| `status.links.dashboard.uid` | `gcx dashboards get <uid>` (metadata/panels); `gcx dashboards search <keywords>` (find related); `gcx dashboards snapshot <uid> --since 6h` (visual inspection) |
| `status.links.slo.uid` | `gcx slo definitions status <uid>` (current SLI + budget — note: budget figure may show 100% if recording rules are absent or budget went deeply negative; use `gcx dashboards snapshot grafana_slo_app-<uid> --since 28d` for the authoritative historical view); then `gcx slo definitions get <uid>` to extract datasource UID and recording-rule queries for `gcx metrics query`; or **slo-investigate** |

### Step 5: Act

The same verb runs single-target (pass `<id>`) or bulk-by-filter (omit `<id>`, pass filter flags). Verbs: `acknowledge`, `unacknowledge`, `silence` (+ `--duration` seconds), `unsilence`, `resolve`, `unresolve`, `delete`. All are idempotent except `delete`.

```bash
# Single-target
gcx irm oncall alert-groups acknowledge <id>
gcx irm oncall alert-groups silence <id> --duration 3600

# Bulk-by-filter (filter flags mirror `list`)
gcx irm oncall alert-groups acknowledge --team <PK> --state firing --yes
```

Result envelopes:

```json
// Single-target
{"action":"acknowledge","target":{"alertGroupId":"<id>"},"changed":true}

// Bulk
{"action":"acknowledge","summary":{"matched":23,"succeeded":18,"skipped":5,"failed":0},"failures":[]}
```

`changed:false` / `skipped++` on idempotent re-runs (not an error). `matched == succeeded + skipped + failed`. `failures[]` carries only errored targets.

Bulk in agent mode REQUIRES `--yes` when matched count > 1. Show the user the filter set and confirm before running — do not pre-fill `--yes`. A bulk call with no `<id>` AND no filter flags is blocked with a structured error containing `suggestions[]`.

## Error Handling

- `<id> argument or filter flag required` — surface the error's `suggestions[]` to the user.
- Agent mode + matched > 1 + no `--yes` — confirm scope with the user, then re-run with `--yes`.
- 404 on `get <id>` — group may have been purged; retry the prior `list` with `--all --state resolved`.
- `webhook` / `formatted_webhook` integrations — partial `status.links.*` and empty `subject.labels` are expected, not errors.

## Tips

- `get` inlines `last_alert.raw_request_data` — prefer it over `list-alerts` for typical triage.
- `subject.labels` is canonical on `get`; on `list` it is HTML-scraped (good enough for the table, not for exact-label work).
- The team cell renders `<name> (<id>)`; the ID is the copy-paste target for `--team`.
- `gcx irm oncall alerts` and `alerts get` were removed — use `alert-groups list-alerts <group-id>`.

## Related Skills

- **investigate-alert** — rule-side root cause once you have `status.links.alert.rule.uid`.
- **slo-investigate** — when `status.links.slo.uid` is populated.
- **debug-with-grafana** — broader investigation when the alert is a symptom of a service-level issue.
