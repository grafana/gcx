---
name: gcx-demo
description: >
  Run a narrated, read-only demo tour of gcx for customer or colleague
  presentations. Showcases the breadth of gcx across every Grafana Cloud
  product area — resources, datasources, metrics, logs, traces, SLOs,
  alerts, synthetic monitoring, IRM, k6, fleet, and more. All commands are
  strictly read-only. Trigger when the user says "demo gcx", "show off gcx",
  "customer demo", "gcx tour", or "/gcx-demo".
user-invocable: true
argument-hint: "[--context <name>]"
allowed-tools: Bash, AskUserQuestion
---

# gcx Demo Tour

Deliver a narrated, read-only showcase of gcx across Grafana Cloud product
areas. All commands are `list`, `get`, `query`, or `status` — nothing is
created, modified, or deleted.

## Principles

- **Read-only only.** Never use `push`, `create`, `update`, `delete`, `edit`,
  or `pull` with local paths. If a command requires write scope, skip it and
  note why.
- **Discover before presenting.** Run exploratory commands first, then narrate
  what's interesting. Let actual output guide emphasis — don't recite a script.
- **Adapt to the stack.** The order, pacing, and which areas to highlight depend
  on what's actually on the stack. Cover what's present; skip what isn't.
- **Parallel by default.** Run independent commands in one message.
- **Narrate what matters.** After each area, explain the value — not the syntax.
- **Graceful degradation.** If a command fails, note it briefly and continue.

---

## Start: Verify Context

Always run this first. If `$ARGUMENTS` contains `--context <name>`, use that
context for all commands. Otherwise use the active context.

```bash
gcx config current-context
gcx config check
```

Announce the target stack before running anything else. If `config check`
fails, stop and ask the user to fix the context.

---

## Coverage Areas

Cover the following areas in an order that builds a coherent story. Run
independent commands in parallel. Let what you find guide what you emphasize
and how much time you spend on each area.

### Provider Landscape

Good opening — shows breadth at a glance.

```bash
gcx providers list
```

### K8s-native Resources

```bash
gcx resources get dashboards -o wide --no-truncate
gcx resources get folders --no-truncate
gcx resources schemas
```

Dashboards are K8s resources — listable, pushable, validateable. The `URL`
column in `-o wide` gives a direct deep link for every dashboard. `gcx
resources schemas` reveals the full type catalog including any plugin-installed
types (e.g. Adaptive Logs `DropRule`). `gcx resources examples <Kind>` produces
a ready-to-push template for provider-registered kinds (try `slos` or
`DropRule`) — core kinds like dashboards and folders don't ship examples, so
don't demo it on those.

### Datasources

```bash
gcx datasources list
```

Every connected datasource — cloud, PDC-tunneled, third-party. All queryable
from the CLI.

### Live Signals

Discover datasource UIDs first (`gcx datasources list -o json`), then query
whichever signal types are present — skip any that aren't on the stack.

```bash
gcx metrics query 'topk(5, count by (job) (up))' -d <PROM_UID>
gcx logs query '{service_name=~".+"}' --limit 5 -d <LOKI_UID>
gcx traces query '{}' --limit 5 -d <TEMPO_UID>
```

Adapt the queries to what's interesting on this stack — pick label selectors,
metric names, or trace filters that will return meaningful output. Mention
`--open` (jump to Grafana Explore) and `--share-link` (shareable URL) as
natural follow-ons.

### Assistant

Cloud-only. Requires `gcx login` (OAuth). Show the commands; run live only if
the stack is healthy and the presenter has time — streaming output has variable
latency.

```bash
gcx assistant prompt "What alerts are firing right now and why?"
gcx assistant prompt "Which service owns checkout-latency?" --continue
gcx assistant prompt "Summarize CPU on prod" --json

gcx assistant investigations list
gcx assistant investigations todos <id>
gcx assistant investigations timeline <id>
gcx assistant investigations report <id>
```

`prompt` runs natural language against the stack's live data — the Assistant
already has context for dashboards, datasources, and alerts. `--continue`
threads follow-ups via a stored context ID. `--json` emits a structured event
stream for agent tools (Claude Code, Cursor) or scripts.

Investigations are autonomous multi-step LLM runs. The read-only views show
plan (`todos`), chronological activity (`timeline`), and final findings
(`report`). `--open` on `investigations get` deep-links into the Grafana UI.

If `investigations list` is empty, note it and skip the per-investigation
views. Do not create one during the demo.

### Reliability & Alerting

```bash
gcx slo definitions status
gcx alert instances list --state firing
gcx alert rules list --no-truncate
```

SLOs return SLI, error budget, and status in one command (add `-o wide` for
burn rate) — scriptable for release gates. Firing alerts include labels,
annotations, and runbook URLs. The rules list shows state and health at a
glance; drill into a single rule with `gcx alert rules get <uid> -o json` when
the audience wants the full PromQL, evaluation timing, or datasource UIDs —
the list table doesn't carry those.

### Synthetic Monitoring

```bash
gcx synth checks list
gcx synth probes list
```

HTTP and browser checks from a global probe network — the probes table shows
each probe's region and online status; `-o json` adds coordinates and
capabilities if the audience asks.

### IRM

```bash
gcx irm oncall schedules list
```

Who's on-call right now. Pipeable into runbooks and automation.

### Cloud Provider Commands

Require `cloud.token` and `cloud.stack`. Skip gracefully if not configured,
and note what's needed.

```bash
gcx k6 load-tests list --no-truncate
gcx fleet pipelines list --no-truncate
```

k6: load test catalog alongside the Grafana stack — correlate test timing with
live metrics. Fleet: full Alloy HCL pipeline configs deployed to collectors via
matcher rules.

---

## Close

Summarize what was actually shown — areas covered, notable findings (a
specific alert, a latency signal in traces, an interesting datasource mix),
and the commands used. Adapt the summary to what was interesting on this
particular stack. Don't recite a fixed table.

Close with: "One binary, one context, every Grafana Cloud product. All
scriptable, all pipelineable — and with `--dry-run` on resource and SLO pushes
for safe GitOps workflows." (Don't claim every mutation verb has `--dry-run`;
the create commands for checks, schedules, and pipelines currently don't.)

---

## Error Handling

| Situation | Action |
|-----------|--------|
| `config check` fails | Stop. Ask user to fix the context before continuing. |
| Signal datasource not found | Skip that signal type, note it. |
| `cloud.token` / `cloud.stack` missing | Skip k6 and fleet, note what's needed. |
| Assistant unavailable (self-hosted, OAuth missing, 403) | Skip assistant section, note Cloud + OAuth requirement. |
| Auth scope missing (403) | Note the missing scope, skip, continue. |
| Empty list (0 resources) | Report "none found" — not an error; continue. |
| Any other command error | Print the error summary, skip the section, continue. |

Never abort for a single failure. Surface errors at the end.
