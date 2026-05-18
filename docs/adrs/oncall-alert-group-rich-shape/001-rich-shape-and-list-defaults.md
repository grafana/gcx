# Rich `AlertGroup` shape and actionable `alert-groups list` defaults

**Created**: 2026-05-05
**Status**: implemented
**Supersedes**: none

## Context

`gcx irm oncall` has two SRE-blocking gaps on real Grafana Cloud stacks:

1. **`alert-groups list` is unusable.** It iterates the entire alert-group history, includes child groups the UI hides via `is_root=true`, and mixes resolved with active state.
2. **`alert-groups list-alerts` returns useless IDs.** The `Alert` type is truncated to `ID, LinkToUpstreamDetails, CreatedAt, RenderForWeb`. The Alertmanager-shape payload (labels, annotations, status, fingerprint, generatorURL) lives only on the per-alert retrieve endpoint; the list endpoint's slim serializer omits it.

Both blockers hit SRE / on-call engineers acting on live alerts, drilling into context, and pivoting between OnCall and Grafana alerting. Output must be predictable for both human and agent invocation: pivot identifiers must be machine-extractable from structured output without string parsing, and the stdout/stderr split is invariant. This ADR addresses both gaps and binds the affected commands to the project's agent-mode output and hint conventions.

## Decision

### 1. `alert-groups list`: actionable defaults

- Default `status` filter: `firing,acknowledged,silenced` (excludes `resolved`).
- `is_root=true` always applied (excludes child groups, matches the UI).
- New filter flags: `--state` (multi), `--team`, `--integration`, `--mine`, `--with-resolution-note`, `--has-related-incident`. `--max-age` retained.
- `--all` shortcut: no status filter, no `is_root` filter — escape hatch for scripted consumers and one-off audits.
- `--include-child-groups`: opts back into child-group inclusion without dropping the status filter.
- Help text documents the default explicitly.

The default flips immediately rather than ramping through an opt-in flag first. gcx is pre-1.0 and the primary consumer is the project author; an opt-in ramp adds release latency without preserving meaningful contracts. The breaking change is documented loudly in CHANGELOG.

### 2. Rich `AlertGroup` and `Alert` shapes with K8s envelope

The OnCall API is asymmetric and the redesign navigates that asymmetry:

- `/alertgroups/<id>/` already inlines `last_alert.raw_request_data`. The 99% SRE drilldown ("what's this alert about") gets the full Alertmanager-shape payload in one round trip.
- `/alerts/?alert_group_id=X` (list) returns a slim shape; `/alerts/<id>/` (retrieve) returns the rich `raw_request_data`.
- The AlertGroup shape itself needs work too — integer enum `status`, primary-key `team`, wall-of-HTML `render_for_web`, and the actually-useful structured fields buried inside `last_alert.raw_request_data`.

Both resources are reshaped under the project's K8s-style envelope (`apiVersion`/`kind`/`metadata`/`spec`/`status`) so meta, configuration, and runtime state are visually separated, with runtime fields grouped hierarchically by what they point at (`subject`/`dimensions`, `links.{alert,dashboard,slo}`, `timestamps`, `raw`) instead of flattened next to opaque IDs.

The shape is centred on Alertmanager's grouping semantics, which applies to ~90% of real groups on the ops stack we sampled: the `AlertGroup` carries the shared `subject` (= `commonLabels`, the slice of labels every alert in the group has in common), and each child `Alert` carries its `dimensions` (= `alert.labels − commonLabels`, the per-fire discriminators). This pairs cleanly with `status.alertsCount` (canonical group-wide alert count) and a per-alert `status.occurrences` (re-fire count under default `list-alerts` collapse).

#### 2.1 `AlertGroup`

```yaml
apiVersion: oncall.ext.grafana.app/v1alpha1
kind: AlertGroup
metadata:
  name: IWDIPP8VLKENJ                         # was pk
  namespace: stacks-27821
  creationTimestamp: "2026-05-05T19:29:23Z"   # was started_at
  labels: {}                                  # OnCall app's user-set labels
spec:
  integration: {id, name, type}               # type drives extraction shape (see §2.3)
  team:
    id: TKH52TW6TH7UE                         # for queries
    name: <resolved>                          # resolved via cached teams list (1 GET per command, then cached)
  permalinks: {web, slack, slack_app, telegram}
status:
  title: "..."
  summary: "..."                              # commonAnnotations.summary or .description
  severity: warning                           # commonLabels.severity
  state: acknowledged                         # decoded enum: 0→firing, 1→acknowledged, 2→resolved, 3→silenced
  runbookURL: "..."                           # commonAnnotations.runbook_url
  subject:
    labels: {...}                             # filtered Alertmanager commonLabels (denylist applied; canonical labels home)
  timestamps: {started, acknowledged, resolved, silenced}
  links:                                      # cross-provider pivot identifiers + URLs (omitempty per block)
    alert:
      rule: {uid, url}                        # uid via fallback chain (§2.3); url = alerts[0].generatorURL
      instance: {id, silenceURL}              # id = alerts[0].fingerprint; silenceURL grafana_alerting only
    dashboard: {uid, url, panel: {id, url}}
    slo: {uid, name}                          # commonLabels.grafana_slo_uuid + commonAnnotations.slo_name
  alertsCount: 3                              # canonical AM-reported group-wide count
  raw: {commonLabels, commonAnnotations, groupLabels}   # hidden by default — see §2.6
```

`status.state` is decoded from the OnCall integer enum into the corresponding string so agents and humans read the same value and `--json` projection works without enum-table lookups. `lastAlert` is intentionally NOT exposed on `AlertGroup` — callers needing per-alert data run `list-alerts <group-id>`.

`status.subject.labels` is the canonical labels home: on `get` it is the post-denylist projection of `status.raw.commonLabels` (which is reachable verbatim under `--include-raw`). On `list` the slim API omits `commonLabels` entirely, so `subject.labels` is best-effort scraped from the `render_for_web` HTML snippet — sufficient for the priority-driven cell rendering in §6 but custom annotation keys with non-standard names may slip through the denylist there. Callers needing the exact canonical labels run `get <id> --include-raw`.

#### 2.2 `Alert`

`Alert` is returned by `list-alerts <group-id>`. Its status block mirrors `AlertGroup`'s and exposes the full Alertmanager-shape group-webhook body under `status.raw` (hidden by default — see §2.6):

```yaml
apiVersion: oncall.ext.grafana.app/v1alpha1
kind: Alert
metadata: {name, namespace, creationTimestamp}
spec:
  alertGroupID: IWDIPP8VLKENJ                 # back-pointer to the parent group
status:
  state: firing                               # from payload.alerts[0].status (per-alert, not the group-wide state)
  severity: warning
  dimensions:
    labels: {...}                             # set difference of alert.labels against parent commonLabels, by value
  occurrences: 4                              # count of stored alerts sharing this label set within the parent group
  links: {alert, dashboard, slo}              # same shape as AlertGroup.status.links — see §2.1
  raw:                                        # hidden by default — see §2.6
    # full raw Alertmanager-shape group webhook (= API's raw_request_data):
    # status, groupLabels, commonLabels, commonAnnotations, groupKey,
    # externalURL, receiver, numFiring, numResolved, truncatedAlerts,
    # alerts[]: {status, labels, annotations, fingerprint, generatorURL, startsAt, endsAt}
```

Each `Alert` record's `raw.alerts[]` array empirically has exactly one entry ~99% of the time; that entry populates the promoted `status.dimensions` and `status.links` fields, which are always present regardless of the `--include-raw` flag. Multi-entry batches lose per-entry distinguishing data in the promoted view; `--include-raw` exposes the full `raw.alerts[]` array as the escape hatch. The underlying retrieve runs even when raw is hidden, so the flag only controls emission — no re-fetch.

`status.dimensions.labels` is computed by value, not by key: a key-value pair appears in `dimensions.labels` only if the alert carries it AND the parent group's `commonLabels` either does not carry the same key or carries a different value for it. Keys whose value matches the parent commonLabels are pruned (they are part of the group's `subject`, not a per-fire dimension). The post-prune set then has the same denylist applied as `subject.labels` (§2.3).

`status.occurrences` counts the stored alerts in the parent group that share this alert's label set. Under default `list-alerts <group-id>` collapse (see §2.4) this is the re-fire count for the labeled instance; under `--history` it is always 1.

`list-alerts <group-id>` defaults to **collapse-by-label-set**: alerts that share an identical `dimensions.labels` set (equivalent to sharing an Alertmanager fingerprint) are folded into a single row whose `status.occurrences` reports the re-fire count. `--history` opts out of the collapse for all output modes (table, wide, yaml, json), surfacing every individual alert delivery — equivalent to the historical raw-alert stream.

#### 2.3 Promoted-field extraction and labels machinery

Two integration shapes diverge in where pivot UIDs live:

- **`grafana_alerting`** (native Grafana 11+): first-class on `alerts[].ruleUID`, `alerts[].dashboardURL`, `alerts[].panelURL`, `alerts[].silenceURL`.
- **`alertmanager`** (Grafana-managed routed via Alertmanager): buried in `alerts[].labels.__alert_rule_uid__`, `alerts[].annotations.__dashboardUid__`, `alerts[].annotations.__panelId__`. There is no `dashboard_uid` label and no `panel_id` label.

Two non-extractable shapes also exist (`formatted_webhook`, `webhook`) that populate few or no promoted fields and typically have empty `commonLabels`; `omitempty` handles them.

##### Pivot links

Each `status.links.*` field is extracted via an ordered fallback chain that walks both shapes. All paths below are relative to `status.links` unless stated otherwise.

| Field | Fallback chain |
|---|---|
| `links.alert.rule.uid` | `alerts[0].ruleUID` → `alerts[0].labels.__alert_rule_uid__` → `commonLabels.__alert_rule_uid__` |
| `links.alert.rule.url` | `alerts[0].generatorURL` |
| `links.alert.instance.id` | `alerts[0].fingerprint` |
| `links.alert.instance.silenceURL` | `alerts[0].silenceURL` (grafana_alerting only) |
| `links.dashboard.uid` | `alerts[0].annotations.__dashboardUid__` → `commonAnnotations.__dashboardUid__` → URL parse from `alerts[0].dashboardURL` (`/d/<UID>/`) |
| `links.dashboard.url` | `alerts[0].dashboardURL` → `alerts[0].annotations.dashboard_url` |
| `links.dashboard.panel.id` | `alerts[0].annotations.__panelId__` → URL parse from `alerts[0].panelURL` (`viewPanel=<ID>`) |
| `links.dashboard.panel.url` | `alerts[0].panelURL` (grafana_alerting only) |
| `links.slo.uid` | `commonLabels.grafana_slo_uuid` → `commonLabels.grafana_slo_uid` |
| `links.slo.name` | `commonAnnotations.slo_name` |
| `severity` | `commonLabels.severity` / `alerts[0].labels.severity` |
| `runbookURL` | `commonAnnotations.runbook_url` / `alerts[0].annotations.runbook_url` |
| `summary` | `commonAnnotations.summary` → `commonAnnotations.description` (first non-empty) |

All promoted fields are `omitempty`. Non-Grafana integrations populate fewer fields, sometimes zero — that is expected behaviour, not an error.

##### `subject.labels` and `dimensions.labels`

`subject.labels` (on `AlertGroup`) and `dimensions.labels` (on `Alert`) are derived from the same source map, then filtered through a shared denylist:

- `subject.labels` = `commonLabels` filtered by the denylist below.
- `dimensions.labels` = the set difference `alert.labels − commonLabels` (by value, see §2.2) filtered by the same denylist.

**Denylist** (applied last, before the labels are emitted):

- *Prefix rule*: any key starting with `__` (two underscores) is dropped. This catches Alertmanager / Grafana internals: `__alert_rule_uid__`, `__grafana_managed_route__`, `__converted_prometheus_rule__`, `__grafana_origin`, `__bypass_*`, `__name__`, `__enriched_by`, etc.
- *Explicit list* (case-sensitive, exact match): `description`, `summary`, `runbook_url`, `message`, `dashboard_url`, `documentation_url`, `playbook_url`, `grafana_folder`, `alertname`, `severity`. These are either annotations that leaked into commonLabels via Grafana's enrichment pipeline, or labels that are already promoted to a structured `status.*` field (`severity` → `status.severity`; `alertname` is the alert rule's name, redundant with `status.title`).

##### Source paths and known limitations

On `get` (and on the per-alert retrieves the rich `list-alerts` performs), `commonLabels` is read directly from the API's `raw_request_data`. On `list`, the slim alert-groups serializer omits `commonLabels` entirely, so `subject.labels` is best-effort scraped from the `render_for_web` HTML snippet. The HTML scrape covers known Grafana-managed-Alertmanager output but rare custom annotation keys with non-standard names may slip through the denylist on the list path (mitigated by the priority-driven cell rendering in §6, which surfaces only known-shape keys). Callers needing the exact canonical labels read them from `status.subject.labels` on a `get` response (or from `status.raw.commonLabels` under `--include-raw`).

The `__*` prefix rule is intentionally one-sided rather than a `__*__` wrap — empirically, Grafana enricher keys like `__enriched_by` and `__grafana_origin` use the prefix-only form, and dropping them is required to keep the `subject.labels` rendering free of internal noise.

#### 2.4 `list-alerts` behaviour

The slim list endpoint matters less than it first appears (`alertgroups/<id>/` already inlines `last_alert.raw_request_data` for typical drilldowns), but `list-alerts` still owns the per-alert detail view, and that view must be rich for the SRE pivot path to work end-to-end.

- **Default**: rich. For each Alert returned by the slim `/alerts/?alert_group_id=X` endpoint, gcx fetches `/alerts/<id>/` (which returns `raw_request_data`) and populates the full shape.
- **Concurrency**: bounded errgroup, default 10 (gcx convention).
- **Cap**: 100 alerts per group with a `warn:` line when exceeded ("retrieved 100 of K alerts; pass `--limit 0` to fetch all"). 100 × ~150ms ≈ 15s upper bound. `--limit 0` removes the cap entirely; `--limit N` sets a different cap.
- **`--slim`**: opt-out flag — skips the N+1 entirely, returning `Alert` objects with no extracted fields and no `status.raw`. Suitable for sorting, counting, or spotting use cases that do not need pivot identifiers.
- **`--include-raw`**: orthogonal opt-in flag — emits `status.raw` with the full Alertmanager-shape webhook for every alert. Fetch behaviour unchanged (the N+1 still runs because the extracted fields require it); this flag only controls what is emitted. See §2.6.
- **Per-alert ordering**: same order as the slim API returns (most-recent-first, matching `-created_at` on the OnCall queryset).

#### 2.5 `alerts get <id>` removed

`AlertRawSerializer` does not return `alert_group_pk`, leaving `spec` empty and orphaning the resource from group context. All real entry points (`list-alerts <group-id>`, web/Slack permalinks formatted as `/alert-groups/<group-id>/...`, upstream notification webhooks) start from group-level data, so the verb is dead weight in practice. The shared rich-shape extraction surface (`GetAlertRich`, `AlertRich` types, envelope converters) is retained — `alert-groups list-alerts <group-id>` uses it for the rich-by-default fan-out.

#### 2.6 `--include-raw`: opt-in raw passthrough

`AlertGroup.status.raw` (commonLabels + commonAnnotations + groupLabels) and `Alert.status.raw` (full Alertmanager-shape webhook with the nested `alerts[]` array) are hidden by default on `alert-groups get` and `alert-groups list-alerts`. The promoted blocks are the curated view of the same data and cover the typical SRE drilldown.

`--include-raw` opts the raw block back in. Common cases: an SRE wants an unpromoted label or annotation (e.g. a custom `team_routing_hint`); a multi-cell investigation enumerates `raw.alerts[*].labels.namespace` because the promoted `dimensions.labels` only captures `payload.alerts[0]`; an agent does arbitrary projection over the full payload.

The flag does not affect fetch behaviour — the underlying retrieves always happen because the extracted fields require the raw payload. Default-off keeps typical output ~50% smaller and free of `__values__` / `__value_string__` / `__alertImageToken__` / `__orgId__` and similar Grafana-internal noise; the flag is a one-character toggle when that data is genuinely needed.

### 3. Agent-mode output contract

#### 3.1 Stream contract

For every mutating command in scope (`alert-groups acknowledge|unacknowledge|resolve|unresolve|silence|unsilence|delete`):

- **stdout** = exactly one JSON document — the result envelope (or the fused error envelope on failure).
- **stderr** = zero or more JSON records, one per line, each with a typed `event` or `class` field. Bulk-by-filter operations emit one progress event per item (`{"event":"acknowledged","target":{...}}`).
- A timeout / partial failure / total failure fuses into the result envelope on stdout — never two documents on stdout.

#### 3.2 MutationResult envelope

Single-target invocations (positional `<id>`) and bulk-by-filter invocations (`--filter` form) emit mutually-exclusive shapes. Single-target = per-resource detail; bulk = aggregate counts plus enumerated failures.

**Single-target** (positional `<id>`):

```json
{"action": "acknowledge", "target": {"alertGroupId": "IKFI..."}, "changed": true}
```

`changed:false` on idempotent re-runs (acknowledge of an already-acked group). On API failure, `changed` is replaced by `error: {code, message, suggestion}` (§3.3); on pre-fetch GET failure, the command exits via canonical DetailedError and emits no MutationResult.

**Bulk-by-filter** (`--filter` form, no positional):

```json
{
  "action": "acknowledge",
  "summary": {"matched": 23, "succeeded": 18, "skipped": 5, "failed": 0},
  "failures": []
}
```

Invariant: `summary.matched == succeeded + skipped + failed`. `succeeded` = newly changed; `skipped` = already in target state (idempotent no-op); `failed` = API call errored. `failures[]` is empty when `failed == 0`, populated only with errored targets — successes are counted, not enumerated. Each failure is `{target: {alertGroupId}, error: {code, message, suggestion}}`.

The two shapes are mutually exclusive: single-target invocations never emit `summary`/`failures`; bulk invocations never emit top-level `target`/`changed`. State-machine verbs (`acknowledge`/`resolve`/`silence`) do NOT emit `fields[]` — that key is reserved for declarative-config writes with field-granular changes.

TTY rendering is decoupled from JSON shape — both shapes render to a one-line human summary in non-agent mode.

#### 3.3 DetailedError + suggestions

Every error path emits the canonical schema:

```json
{
  "error": {
    "summary": "<one-line summary>",
    "exitCode": 1,
    "details": "<structured multi-line detail>",
    "suggestions": ["<runnable command>", "<runnable command>"]
  }
}
```

Errors from the OnCall plugin proxy are translated into this shape — backend 500s do not leak through with raw call chains. The "id-or-filter required" guardrail on bulk action verbs ships with concrete suggestions:

```json
{
  "error": {
    "summary": "<id> argument or filter flag required",
    "exitCode": 2,
    "details": "Bulk action verbs require either a positional <id> or one or more filter flags to scope the operation. Acting on every alert group is not supported.",
    "suggestions": [
      "Pass an alert-group ID: gcx irm oncall alert-groups acknowledge <id>",
      "Filter by team: gcx irm oncall alert-groups acknowledge --team <name>",
      "Filter by status + age: gcx irm oncall alert-groups resolve --status firing --max-age 24h"
    ]
  }
}
```

#### 3.4 List envelope, field-select, identity

- `alert-groups list` and `alert-groups list-alerts` return `{"items":[...]}` on stdout. Empty → `{"items":[]}`. Never bare `[...]`, never `null`.
- The new `Alert` and `AlertGroup` fields participate in the global `--json` codec: `--json list` enumerates field paths including `status.links.alert.rule.uid`, `status.links.alert.instance.id`, `status.links.dashboard.uid`, `status.links.dashboard.panel.id`, `status.links.slo.uid`, `status.subject.labels` (AlertGroup) / `status.dimensions.labels` (Alert), `status.severity`, `status.runbookURL`, `status.alertsCount` (AlertGroup) / `status.occurrences` (Alert), etc. Paths under `status.raw.*` participate when `--include-raw` is in effect. Unknown fields exit 2 with a structured error and a suggestion to run `--json list`. Empty-list projection preserves the `{"items":[]}` shape — never a phantom `{"field":null}` row.
- Filter flag names on bulk action verbs match `alert-groups list` exactly (`--team`, `--integration`, `--status`, `--max-age`, `--mine`).
- In agent mode (`--agent` or auto-detected), bulk action verbs MUST fail-fast pre-prompt with a structured DetailedError suggesting `--yes` rather than auto-confirming. Auto-confirmation of destructive operations is a footgun; an explicit `--yes` from the agent's prompt is the safer default.

### 4. Hint conventions

#### 4.1 Three classes, plain-text prefixes

Diagnostic output uses three plain-text prefixes, in strict rendering order:

1. `warn:` — something is off but the operation succeeded (or partially succeeded).
2. `note:` — supplementary information about the result (e.g., "default filter excludes resolved groups; pass --all for full set").
3. `hint:` — concrete next-step suggestions. Each hint MUST be a runnable command.

`warn` → `note` → `hint`, never interleaved.

#### 4.2 Channel discipline

Hints, notes, and warnings always go to **stderr**. Stdout is the result envelope (a single JSON document in agent mode; formatted output in TTY mode). The form of the stderr stream depends on mode:

- **TTY mode**: stderr is plain prefixed text, dim-styled.
  ```
  hint: See live instances: gcx alert instances list --rule <status.links.alert.rule.uid>
  hint: Inspect the rule: gcx alert rules get <status.links.alert.rule.uid>
  note: 0 results — defaults exclude resolved/child groups; try --all
  warn: Default filter excluded 47 resolved groups
  ```
- **Agent mode**: stderr is JSONL — one JSON record per line, with a typed `class` field and structured fields.
  ```jsonl
  {"class":"warning","summary":"Default filter excluded 47 resolved groups"}
  {"class":"note","summary":"0 results — defaults exclude resolved/child groups; try --all"}
  {"class":"hint","summary":"See live instances","command":"gcx alert instances list --rule <status.links.alert.rule.uid>"}
  {"class":"hint","summary":"Inspect the rule","command":"gcx alert rules get <status.links.alert.rule.uid>"}
  ```

In agent mode the JSONL stderr stream is the same channel as the progress events from §3.1 — both are structured records on stderr, distinguished by `event` (progress) vs `class` (diagnostic). Stdout in agent mode remains a single JSON document — the result envelope only.

Suppression flags (`--quiet`, `--no-hints`) are out of scope for this ADR.

#### 4.3 Post-result hints

Discovery verbs (`list`, `get`, `query`) emit a post-result hint suggesting the next logical step on success only, conditional on result content:

| Command | Result | Post-result hint |
|---|---|---|
| `alert-groups list` | non-empty | `hint: Drill into alerts: gcx irm oncall alert-groups list-alerts <group-id>` |
| `alert-groups list` | empty | `hint: 0 results — defaults exclude resolved/child groups; try --all or --include-child-groups` |
| `alert-groups list-alerts` | non-empty | `hint: See live instances: gcx alert instances list --rule <status.links.alert.rule.uid>; inspect rule: gcx alert rules get <status.links.alert.rule.uid>; open dashboard: gcx resources get dashboards/<status.links.dashboard.uid>` |
| `alert-groups get` | success | `hint: See live instances: gcx alert instances list --rule <status.links.alert.rule.uid>; open dashboard: gcx resources get dashboards/<status.links.dashboard.uid>; per-alert detail: gcx irm oncall alert-groups list-alerts <id>` |

Hint emission is conditional on result content — empty vs non-empty produce different hints.

#### 4.4 Errors carry their own suggestions

Error paths use the DetailedError `suggestions[]` array on stdout (§3.3) — they do NOT emit `hint:` lines on stderr. The `suggestions[]` array IS the error-mode hint mechanism. Output-time `hint:` is for success paths.

### 5. Hint and Cost annotations (registry)

The agent command-annotations registry (Cobra-time metadata, distinct from the output-time `hint:` lines in §4) is updated for the SRE-side commands touched here:

| Command | Hint | Cost |
|---|---|---|
| `alert-groups list` | "default filter excludes resolved + child groups; pass `--all` for full set." | small (large with `--all`) |
| `alert-groups get` | "populates rich status with `rule.uid` + `dashboard.uid` + `subject.labels` — pivot identifiers in one round trip; no need to call `list-alerts` for typical drilldown." | small |
| `alert-groups list-alerts` | "extract `status.links.alert.rule.uid` for `gcx alert rules get` / `gcx alert instances list --rule`; `status.links.dashboard.uid` for `gcx resources get dashboards/<uid>`; `--slim` to skip per-alert N+1." | medium (10 concurrent retrieves up to 100 cap; small with `--slim`) |
| `alert-groups acknowledge` (and siblings) | "bulk via filter flags; `--yes` to skip the count-confirmation prompt; agent mode requires `--yes` explicitly." | small (medium with broad filter) |

### 6. Table views

`-o table` (TTY default) and `-o wide` are the human-facing renderings of the K8s envelope. Default columns aim at "what's firing, how bad, who owns it"; wide adds the cross-provider pivot identifiers and the per-group/-alert label set an SRE needs to chain into the next command.

Conventions across both tables:

- `AGE` is a compact relative age (`just now`, `Nm ago`, `Nh ago`, `Nd ago`, `Nw ago`) sourced from `metadata.creationTimestamp`; absolute timestamps live in YAML/JSON output only. AGE applies to `alert-groups list` only — it is omitted from `list-alerts` because the upstream OnCall `alertAPI.CreatedAt` is empty (see §6.2 + Consequences); the column will return when the API exposes per-alert timestamps.
- The `TEAM` cell is rendered `<name> (<id>)`; on overflow the **name is truncated** and the **`<id>` is preserved verbatim** (the team ID is a copy-paste target for `--team-id` flags elsewhere in the CLI).
- Long titles and long UID cells are rune-aware truncated with `…`; URL columns are flex-width and wrap rather than truncate.
- `-o wide` may render specific cells (`SUBJECT`, `DIMENSIONS`) as **multi-line** — one `key=value` per line in priority-then-alpha order, denylist applied. Other cells in the same row anchor to the top line. Rows are separated by a blank line.
- Empty `subject.labels` (e.g. `formatted_webhook` integrations on the list path, where commonLabels is empty in the upstream payload) renders as `-`.
- Empty result sets render no table — the post-result `hint:` line on stderr (§4.3) carries the "0 results" diagnostic.

#### 6.1 `alert-groups list`

Default — identity, severity, state, ownership, subject, recency:

| Column | Source |
|---|---|
| `ID` | `metadata.name` |
| `TITLE` | `status.title` (ellipsis-truncated) |
| `SEVERITY` | `status.severity` |
| `STATE` | `status.state` (decoded enum string) |
| `TEAM` | `spec.team`, rendered `<name> (<id>)` (ID preserved on overflow) |
| `SUBJECT` | `status.subject.labels` rendered via the C3 picker (single highest-priority key + `(+N)` count for remaining post-denylist keys; ellipsis-truncated to fit cell budget) |
| `AGE` | `metadata.creationTimestamp` (relative age) |

`SUBJECT` priority list: `service > job > workload > app > deployment > component > namespace > cluster > container > [alphabetical for the rest]`. The denylist from §2.3 applies before the priority lookup, so `__*`-prefixed and explicit-list keys never count toward the picked key or the `+N` tail.

```
ID             TITLE                          SEVERITY  STATE     TEAM                          SUBJECT                          AGE
I31BVVJPD29B5  KafkaConsumerGroupStuck        warning   firing    appcore-squad (T68S4GX7ZLZRL) job=kafka (+2)                    2m ago
IHBXNKXA9G5C9  UndersizedMemory               warning   firing    loki_Ingest-Query (TIRSPU75…) job=loki (+3)                     13m ago
I1AJL7JDB6TQJ  Delivery Hero new ticket       -         firing    -                             -                                 10m ago
```

Wide — adds the per-group alert count, the rule pivot, and a multi-line rendering of `subject.labels`:

| Wide column | Source |
|---|---|
| `ID` | `metadata.name` |
| `TITLE` | `status.title` (ellipsis-truncated) |
| `TEAM` | `spec.team` (`<name> (<id>)`, ID preserved on overflow) |
| `SEVERITY` | `status.severity` |
| `STATE` | `status.state` |
| `RULE` | `status.links.alert.rule.url` (or `-`) |
| `SUBJECT` | `status.subject.labels` rendered multi-line — one `key=value` per line in priority-then-alpha order, denylist applied |
| `ALERTS` | `status.alertsCount` |
| `AGE` | `metadata.creationTimestamp` |

The wide layout is the first place in gcx where a table cell may render multi-line; documented as a precedent for future column designs.

#### 6.2 `alert-groups list-alerts <group-id>`

Per-alert rows inside an already-known group. The default rendering **collapses by label set** (equivalent to AM fingerprint): alerts that share an identical `dimensions.labels` set fold into a single row whose `COUNT` reports the re-fire count. `--history` opts out of the collapse for all output modes.

The default columns omit `TITLE` / `RULE` / `DASHBOARD` / `SERVICE` / `CLUSTER` / `NAMESPACE` — the parent group's title is already known to the caller (it was in the `list` row they came from), the rule and dashboard pivots are constant across the group's alerts and rendered once on the parent (`alert-groups get`), and a static `service/cluster/namespace` triple proved redundant or misleading on real data (§Rejected Alternatives, "Static `target.*`"). The result is a narrow, scannable table that surfaces what differs row-to-row: identity, state, dimensions, re-fire count.

Default:

| Column | Source |
|---|---|
| `ID` | `metadata.name` |
| `STATE` | `status.state` (per-alert, not group-wide) |
| `DIMENSIONS` | `status.dimensions.labels` rendered via the C3 picker (single highest-priority key + `(+N)`; ellipsis-truncated) |
| `COUNT` | `status.occurrences` (re-fire count under default collapse; always 1 under `--history`) |

`DIMENSIONS` priority list: `pod > instance > tenant > slug > name > container > deployment > [alphabetical]`. Denylist from §2.3 applies before the priority lookup.

```
ID             STATE     DIMENSIONS                          COUNT
A61154KIR7PF8  firing    pod=parallel-querier-…-4wfw7        4
A99XJZ12345YZ  firing    pod=parallel-querier-…-hlr6r        2
```

Wide — adds the rule pivot and a multi-line rendering of `dimensions.labels`:

| Wide column | Source |
|---|---|
| `ID` | `metadata.name` |
| `STATE` | `status.state` |
| `RULE` | `status.links.alert.rule.url` (single-line URL; or `-`) |
| `DIMENSIONS` | `status.dimensions.labels` rendered multi-line — one `key=value` per line in priority-then-alpha order, denylist applied |
| `COUNT` | `status.occurrences` |

`AGE` is intentionally omitted from both default and wide layouts because the upstream OnCall API returns empty `alertAPI.CreatedAt` for every alert (recorded as a known gap; the column will return when the API exposes per-alert timestamps — see Consequences). `metadata.creationTimestamp` on `Alert` is therefore unset in `-o yaml` / `-o json` as well, not just hidden in the table.

### 7. Output codec compliance

`alert-groups` commands MUST comply with the project's constitution and design rules for command output. The default human-facing output for `list`, `get`, and `list-alerts` is the registered `text` (table) codec; agent mode flips the default to the built-in `agents` codec; `json`, `yaml`, and `agents` codecs are built-in and produce content structurally identical to the rich shape. The accompanying spec captures the per-FR detail; the ADR records only the invariant.

## Rejected Alternatives

### Static `target.{service, cluster, namespace}` promoted fields

Earlier drafts treated `service`, `cluster`, and `namespace` as canonical promoted fields at `status.target.*`. Real-data validation on the ops stack (200-group sample) showed `service` populated for 3% of groups and the `target.*` block uniformly empty for non-`alertmanager` integrations (10% of the sample). Shipped reality: drop `target.*` entirely; introduce `status.subject.labels` (on `AlertGroup`) and `status.dimensions.labels` (on `Alert`) carrying whatever the underlying Alertmanager grouping schema actually used. The C3 picker (§6) renders the single highest-priority key per cell, falling back through the priority list, so the table view adapts to whatever keys are present rather than assuming a static `service/cluster/namespace` triple.

### `status.uniqueCount` field on `AlertGroup`

Considered as a count of distinct fingerprints after `list-alerts` collapse — useful as a hint of how many genuine entities a group spans. Computable on `get` (full payload available) but not on `list` (the slim serializer omits raw, and the alert list is not embedded in the group response). Rather than ship a partially-populated field whose meaning depends on which verb produced it, the field was dropped entirely. `status.alertsCount` (the AM-reported group-wide count) remains the canonical surface; the per-row `status.occurrences` on collapsed `list-alerts` rows captures the re-fire count where it actually matters.

### `open` as a sub-verb (`alert-groups open <id>`)

`--open` is the established mechanism on the generic `gcx resources get` command. Sibling sub-verbs duplicate the affordance and bloat the command tree.

### Nested `relatedAlerting` block

```json
{ "id": "...", "relatedAlerting": { "ruleUID": "...", "ruleURL": "...", ... } }
```

Rejected in favour of typed sub-blocks grouped under `status.links` by what-they-point-at (`links.alert.{rule,instance}`, `links.dashboard`, `links.slo`). Pairing each ID with its URL inside a sub-block reads more naturally than a flat parallel `alertRuleUID`/`alertRuleURL` arrangement. Grouping all cross-provider pivots under a single `links` umbrella makes the section easy to find and lets each block evolve independently (e.g. add `links.synthetic` for synthetic monitoring later) without inventing a new top-level surface every time. Agents and codecs still read scalar fields directly — `status.links.alert.rule.uid` is one path traversal, no costlier than a flat scalar.

### Retain `alerts get <id>`

Considered as the cheapest single-alert path. Rejected because `AlertRawSerializer` omits `alert_group_pk`, leaving `spec` empty and orphaning the resource from group context. All real entry points (`list-alerts`, permalinks, webhooks) start from group-level data — there is no realistic flow where a caller holds a bare alert ID without already knowing the parent group. The verb was removed; the shared rich extraction surface stays because `list-alerts` uses it.

### `bulk-acknowledge` / `bulk-resolve` / `stats` sub-verbs

Bulk operations belong on the existing action verb (with filter flags), not a parallel `bulk-*` family. `stats` is covered by `list -o table` aggregation.

## Consequences

### Positive

- Both pain points are resolved end-to-end: actionable list defaults, and rich `AlertGroup` / `Alert` shapes (K8s envelope, hierarchical `status.{subject|dimensions, links, timestamps}` blocks, decoded `state` enum, no HTML `render_for_web`).
- SRE workflow gains a coherent "investigate → pivot to alerting → act" path through promoted cross-provider IDs.
- `subject.labels` / `dimensions.labels` make the AlertGroup-vs-Alert relationship faithful to Alertmanager's grouping semantics — the table view adapts to the actual grouping schema (whatever the rule chose to group by), not a hardcoded `service/cluster/namespace` triple. Empirically (§Rejected Alternatives, "Static `target.*`") this matters: `service` was populated for only 3% of groups, while the labels actually used for grouping vary widely across teams.
- The tailored tier no longer diverges in shape from the generic tier — one decoder per resource works across both tiers.
- Agent-mode behaviour for the affected commands is unified: stream discipline, MutationResult envelope, DetailedError with suggestions, `{"items":[]}` list envelope, `--json` validator, `--yes` consistency, and structured JSONL diagnostics on stderr.
- Bulk operations on the alert-groups action verbs compose via filter flags — no parallel `bulk-*` verb family.

### Negative

- **Breaking change** to `alert-groups list` default behaviour. Anyone with scripts that grepped resolved alert groups out of the default output must add `--all` (or migrate to `--state resolved`). Documented in CHANGELOG.
- **Breaking change** to `AlertGroup` and `Alert` output shape: K8s envelope split (`metadata`/`spec`/`status`), decoded `status.state` string (was integer), dropped `render_for_web`, dropped `status.target.*`, introduced `status.subject.labels` (AlertGroup) / `status.dimensions.labels` + `status.occurrences` (Alert), and renamed/restructured promoted fields grouped hierarchically under `status.links` rather than flattened. Scripts that grepped the old flat YAML break. CHANGELOG covers the migration with worked examples.
- **Breaking change**: `gcx irm oncall alerts get <id>` is removed (§2.5). Callers should use `alert-groups list-alerts <group-id>` instead.
- **Breaking change** to alert-group action-verb output envelopes: action verbs return the two-shape MutationResult; errors return DetailedError. Scripts that grepped the old human-readable output break. Migration cost is one-time and documented in CHANGELOG.
- On the `list` path, `subject.labels` is best-effort scraped from `render_for_web` because the slim API omits `commonLabels`; rare custom annotation keys may slip through the denylist. Mitigated by the priority-list-driven cell rendering in §6 (which only surfaces known-label-shape keys) and by the canonical commonLabels being available verbatim via `get` (or `get --include-raw` for the unfiltered map). Removing the HTML scrape requires backend enrichment of OnCall's slim alert-group serializer to include `commonLabels` — already on the IRM-team radar.
- `list-alerts` has no `AGE` column because the upstream OnCall response returns empty `alertAPI.CreatedAt` for every alert. The column will return when the OnCall API exposes per-alert timestamps; until then `metadata.creationTimestamp` on `Alert` is also unset in `-o yaml` / `-o json`.
- The Alertmanager-shape promotion in `Alert` couples gcx to Grafana-managed alert label conventions. Non-Grafana integrations (`webhook`, `formatted_webhook`) get partial promotion and typically have empty `subject.labels` / `dimensions.labels`. Mitigation: `omitempty` keeps the JSON honest; `status.raw` (when `--include-raw` is passed) preserves the full payload.
