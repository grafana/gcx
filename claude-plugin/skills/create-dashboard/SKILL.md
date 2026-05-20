---
name: create-dashboard
description: >
  Use when the user wants to create a new Grafana dashboard, design dashboard
  panels, variables, queries, or layout, or make a material visual redesign of
  an existing dashboard. This skill uses gcx plus `gcx dashboards snapshot` as
  a visual feedback loop. Triggers on "create dashboard", "new dashboard",
  "build dashboard", "dashboard for <service>", "improve this dashboard", or
  "iterate on a dashboard".
---

# Create Dashboard

Create dashboards as an iterative product workflow, not as a one-shot manifest
write. The loop is:

```text
understand goal → discover data → author dashboard → validate/push → snapshot → inspect → revise
```

A dashboard creation task is not complete after `push` succeeds. It is complete
only after the dashboard has been visually checked, or after you report exactly
why the snapshot step is blocked.

For pure operations on existing dashboards — list, search, pull, push, promote,
restore, delete, or one-off snapshot — use `manage-dashboards` instead.

## Hard Rules

- Do not create dashboards in an arbitrary/default folder. If the target
  context or folder is ambiguous and the operation will write to Grafana, ask
  one concise question.
- If the user gives a context, folder, datasource, or dashboard title, proceed
  with stated assumptions instead of running a long clarification round.
- Verify the active gcx context before writes.
- Verify datasources, metrics, labels, and log fields before adding panels. Do
  not invent PromQL, LogQL, datasource UIDs, label names, or folder UIDs.
- Prefer existing project conventions. If the repo already has
  grafana-foundation-sdk builders, generated manifests, or a dashboards
  registry, follow that pattern.
- Use `gcx dashboards snapshot` for visual feedback. After rendering a PNG,
  inspect the image with the available image/read tool; do not only report the
  file path.
- Iterate at least once after the first snapshot when there are obvious visual
  or data problems: login page, wrong variables, empty prime panels, title
  truncation, broken layout, unreadable legends, or poor top-screen triage.
- Prefer dedicated gcx commands over `gcx api`. Use raw API only when a needed
  operation has no dedicated command.

## Inputs to Establish

Collect only the missing inputs needed to start:

| Input | Needed for | Default if user is vague |
|-------|------------|--------------------------|
| Grafana context | Any read/write | Active gcx context after `gcx config current-context` |
| Folder | Dashboard creation | Ask; do not default to General silently |
| Audience | Dashboard shape | On-call/SRE triage audience |
| Entity scope | Queries/variables | Service/team/namespace from the user request |
| Datasource(s) | Panel queries | Infer from similar dashboards, then verify |
| Time range | Snapshot and query tests | `--since 6h` for operational dashboards |
| Storage path | Local files | Existing repo convention, else `./resources/dashboards/<name>.yaml` |

## Workflow

### 1. Verify context and permissions

```bash
gcx config current-context
gcx config check
gcx config list-contexts
```

Use `--context <name>` on subsequent commands when the user named a context and
you do not want to mutate global state.

Classify failures before continuing:

- config/connectivity failure → use `setup-gcx` first
- read 401/403 → missing dashboard/datasource read permissions
- write 403 → token lacks dashboard write permissions
- folder-specific write failure → wrong folder UID or folder permissions

### 2. Resolve folder and find local/live conventions

Find the target folder and similar dashboards before authoring.

```bash
# Folder/resource inventory. Use JSON for exact UIDs.
gcx resources get folders -o json

# Similar live dashboards are better than generic examples for variables,
# datasource UIDs, tags, units, and team conventions.
gcx dashboards search "<service-or-team-keyword>" -o json
gcx dashboards get <similar-dashboard-uid> -o json > /tmp/similar-dashboard.json
```

Extract useful patterns:

```bash
jq -r '.. | objects | select(.expr?) | .expr' /tmp/similar-dashboard.json | sort -u
jq '.spec.templating.list[]? | {name,type,query,current}' /tmp/similar-dashboard.json
jq '.spec.panels[]? | {id,title,type,gridPos}' /tmp/similar-dashboard.json
```

If local repository scans are appropriate under project rules, look for existing
dashboard builders/manifests and follow their style instead of creating a new
parallel convention.

### 3. Discover data before panels

Use the `explore-datasources` skill when datasource choice or metric inventory is
unclear. Minimum discovery for Prometheus/Loki dashboards:

```bash
gcx datasources list -o json

# Prometheus: prove metric and label availability over the intended scope.
gcx datasources prometheus query -d <prom_uid> '<promql>' --since 5m -o json

# Loki: sample log streams and parsed fields before adding log panels.
gcx datasources loki query -d <loki_uid> '<logql>' --since 1h -o raw
```

Check units, histogram buckets, status labels, route/operation labels, cluster
or namespace labels, and cardinality. Prefer `topk()` or aggregations for high
cardinality dimensions.

### 4. Design the dashboard story

Dashboards should answer a diagnostic question. Prefer a top-to-bottom story:

```text
Context and health → user impact → traffic/errors/latency → dependency path → resources → logs/traces links
```

Panel rules:

- Put the most actionable row in the first screen.
- Use variables for the entity scope users will actually change: cluster,
  namespace, service, route, tenant, datasource.
- Give every panel a clear title, unit, legend, and short description.
- Use thresholds only where they encode an operational decision.
- Collapse deep-dive/noisy rows by default.
- Avoid panels that are empty by design in the normal case unless the panel
  title/description says that empty is healthy.
- Add drilldown links when a panel naturally leads to logs, traces, or another
  dashboard.

Dashboard quality checklist, distilled from Grafana dashboard best practices:

- Define the dashboard's job before adding panels. If a panel does not support
  that job, remove it or move it to a collapsed deep-dive row.
- Prefer fewer, decision-oriented panels over metric inventory. The top row
  should answer "is there a problem and where should I look next?"
- Use consistent units, decimals, color semantics, thresholds, and legend names
  across panels.
- Keep variables useful but limited. Too many variables make snapshots and
  debugging ambiguous.
- Avoid unbounded/high-cardinality queries in default panels. Use aggregation,
  `topk()`, scoped variables, and sensible time ranges.
- Tune refresh and time ranges for the use case. Do not make expensive panels
  auto-refresh faster than the data or the user decision requires.
- Make empty states intentional: either empty means healthy and the description
  says so, or the panel should be fixed/removed.
- Validate readability at the expected viewport, not only the JSON structure.

### 5. Author the dashboard

Choose one authoring mode.

#### Mode A: Existing resources-as-code project

Follow the repo's established pattern. If a typed stub is needed, use the
`generate-resource-stubs` skill or:

```bash
gcx dev generate dashboards/<dashboard_file>.go
```

Then edit the generated builder and any registry/all-files required by the
project. Run the project's normal generation/build command before pushing.

#### Mode B: Standalone resource manifest

Start from the live schema instead of guessing fields:

```bash
gcx resources schemas dashboards -o json > /tmp/dashboard-schema.json
```

`gcx resources examples` is useful for resource types that ship examples, but
current dashboard authoring should not depend on an example being available.

Create a manifest such as `./resources/dashboards/<dashboard-name>.yaml`.
Use a stable slug-like `metadata.name` for the dashboard resource name/UID and a
human-readable `spec.title` for the displayed title.

```yaml
apiVersion: dashboard.grafana.app/v1beta1
kind: Dashboard
metadata:
  name: <stable-dashboard-slug>
spec:
  title: "<Human readable dashboard title>"
  tags: [gcx]
  timezone: browser
  schemaVersion: 36
  # folderUID: <folder-uid>
  templating:
    list: []
  panels: []
```

Fill in panels, variables, links, and layout using patterns discovered above.

### 6. Validate, dry-run, push, and verify

```bash
gcx resources validate -p <dashboard-path-or-dir> -o json
gcx resources push -p <dashboard-path-or-dir> --dry-run
gcx resources push -p <dashboard-path-or-dir>

# Verify the stored object. <name> is metadata.name.
gcx resources get dashboards/<name> -o json
```

If pushing into a directory that also contains folders, gcx orders folders before
dashboards automatically. If an existing dashboard is protected by a different
manager annotation, stop and explain; only use `--include-managed` when the user
explicitly wants gcx to take ownership.

### 7. Snapshot, inspect, and iterate

Render a full-dashboard snapshot with the same variables and time range a user
would use. Replace or omit the `--var` examples below so they match variables
actually defined in the dashboard.

```bash
mkdir -p ./snapshots
GCX_AGENT_MODE=true gcx dashboards snapshot <dashboard-name> \
  --output-dir ./snapshots \
  --since 6h \
  --var cluster=<cluster> \
  --var namespace=<namespace> \
  --width 1920 \
  --theme dark
```

Then read/open the PNG returned in `file_path`. Inspect for:

- login/error page instead of the dashboard
- wrong dashboard, wrong folder, wrong time range, or wrong variables
- empty top-row panels caused by bad queries or labels
- title truncation or legends consuming the panel
- overlapping panels, gaps, or poor row order
- unreadable high-cardinality series
- missing units/thresholds/descriptions
- too much chrome; consider a larger width or kiosk-style settings if available

Render individual panels when a panel is suspect:

```bash
GCX_AGENT_MODE=true gcx dashboards snapshot <dashboard-name> --panel <panel-id> \
  --output-dir ./snapshots --since 6h --width 1200 --height 700
```

If the snapshot reveals issues, edit the source, validate, push, and snapshot
again. Keep the loop short but real; one good visual correction is better than a
successful push with an unusable dashboard.

### 8. Final response

Report only the useful operational facts:

- assumptions made
- Grafana context and folder
- dashboard title and resource name/UID
- local source path
- validation/push result
- snapshot path(s) and what you observed
- iterations performed
- remaining issues or blockers

If snapshot failed, include the exact failure class: missing image renderer,
auth/RBAC, bad variables, rendering timeout, or command error. Do not claim the
dashboard was visually reviewed when it was not.
