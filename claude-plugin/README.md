# gcx Claude Code Plugin

A Claude Code plugin that gives AI agents deep knowledge of gcx — the
kubectl-style CLI for managing Grafana resources. With this plugin, Claude can
set up gcx, scaffold resources-as-code projects, generate and import Grafana
resources, manage dashboards, explore datasources, investigate alerts, debug
live systems, work with SLOs and Synthetic Monitoring, and drive full GitOps
workflows without hand-holding.

## Prerequisites

- [Claude Code](https://claude.ai/claude-code) installed
- Grafana 12+ instance with API access

gcx will be installed by the `setup-gcx` skill if not already
present (requires Go v1.24+).

## Installation

Run these two commands inside Claude Code:

```
/plugin marketplace add grafana/gcx
/plugin install gcx@gcx-marketplace
```

The first command registers this repository as a marketplace. The second
installs the plugin from it. Claude Code will pick it up immediately — no
restart needed.

To update the plugin later:

```
/plugin marketplace update gcx-marketplace
/plugin install gcx@gcx-marketplace
```

## Quick Setup

Once the plugin is installed, ask Claude to configure gcx:

```
/setup-gcx
```

This skill walks through creating a named context pointing at your Grafana
instance, verifying connectivity, and confirming your credentials are working.

## Skills

`claude-plugin/skills/` is the current canonical portable Agent Skills bundle
for gcx. The Claude plugin consumes that tree directly today, and the generic
`.agents` installer exposed by `gcx agent skills install` reads from the same source.

Claude-specific packaging remains under:

- `.claude-plugin/` — plugin manifest and marketplace metadata
- `agents/` — Claude-facing specialist personas

Do not add distributable gcx skills under repo-local `.agents/skills/`. Tools
that follow the `.agents` convention treat that path as repo-context guidance
for working on this repository, not as a globally installable skill bundle.

Skills are triggered automatically when you describe what you want. You do not
need to invoke them by name. The table below is the current inventory of the
canonical portable skill bundle.

| Skill | Purpose |
|-------|---------|
| `setup-gcx` | Install gcx if needed, configure authentication, and verify connectivity to Grafana |
| `gcx` | Use gcx as the default control plane for Grafana resources and queries |
| `scaffold-project` | Scaffold a new gcx resources-as-code project |
| `generate-resource-stubs` | Generate typed Grafana resource stubs as Go code |
| `import-dashboards` | Import existing Grafana dashboards into Go builder code |
| `create-dashboard` | Design and create dashboards with datasource discovery and snapshot-based visual iteration |
| `manage-dashboards` | Operate existing dashboards: list, search, pull, push, validate, promote, restore, delete, and snapshot |
| `investigate-alert` | Investigate why a Grafana alert is firing and what it impacts |
| `oncall-triage` | Triage active Grafana OnCall alert groups: list, inspect, acknowledge, silence, resolve |
| `debug-with-grafana` | Investigate with metrics, logs, and traces; use baseline candidates and trace diff to localize request regressions |
| `diagnose-entity-graph` | Diagnose Knowledge Graph problems: missing entities, missing edges, broken trace context propagation, service-name collisions |
| `slo-check-status` | Check SLO health and summarize current status |
| `slo-investigate` | Diagnose why a specific SLO is breaching or alerting |
| `slo-manage` | Create, update, pull, push, and delete SLO definitions |
| `slo-optimize` | Analyze SLO trends and recommend objective or alerting improvements |
| `agento11y` | Browse conversations, manage evaluators and rules, set up online evaluation for LLM quality scoring (Agent Observability) |
| `agento11y-instrument` | Set up and instrument a developer's own LLM app to send generations to Agent Observability, verifying data lands via gcx |
| `agento11y-prod-setup` | Set up production evaluation and guardrails (online eval rules + guards) for a deployed agent, grounded in its code and live traffic, applied via gcx with confirmation |
| `agento11y-test-starter` | Build a starter offline test suite for an agent before it ships — test cases (Agent Observability suite YAML + runner stub) plus how to score them, grounded in the agent's own code |
| `synth-check-status` | Check Synthetic Monitoring health, status, and trends |
| `synth-investigate-check` | Diagnose why a Synthetic Monitoring check is failing |
| `synth-manage-checks` | Create, update, pull, push, and delete Synthetic Monitoring checks |
| `gcx-observability` | Roll out end-to-end observability: instrumentation, SLOs, alerts, synth, k6, IRM, dashboards, and cost optimization |
| `gcx-demo` | Run a narrated, read-only demo tour of gcx across every Grafana Cloud product area — for customer or colleague presentations |

## Skill Lifecycle

`skills-catalog.yaml` records release metadata independently of the skill files:

- **active:** bundled and available for installation and updates.
- **deprecated:** still bundled; install, get, and update report a warning.
- **retired:** no bundled content; existing local copies remain visible and
  removable with `gcx agent skills uninstall`, including `--all --yes`.

Entries may include an optional `replacement` skill name and `message` explaining
what changed. Replacements are informational labels, not redirects; gcx does not
follow replacement chains. Every bundled skill needs an active or deprecated
entry. When removing content, change its entry to retired and **keep it
indefinitely** so users can skip releases. `TestBundledCatalog` checks catalog/content
consistency and replacement existence against the embedded release. These checks
run only in tests so a packaging mismatch cannot block uninstall.

For the `.agents` installer, commands reconcile the catalog with the selected
`--dir` (default `~/.agents`). `list` includes bundled skills and locally present
retired entries, including incomplete installations without `SKILL.md`. `update`
refreshes installed bundled skills and reports retired copies without changing
them. It never installs replacements automatically or prunes obsolete local files.
Install/update receipts include lifecycle notices in JSON as well as warnings on
stderr. `get` reads only bundled content, not a local copy.

Directories absent from the catalog are unmanaged and never targeted. Reconciled
state records catalog membership in `known`, separate from the release `status`.
Per-skill stat failures report `installed: false` without blocking inventory or
operations on other skills. Catalog names do **not** establish ownership of local files:
there is no installation receipt or local-modification tracking yet. Explicit
uninstall removes the selected directory and its local edits. These lifecycle
rules apply to `gcx agent skills`; the Claude plugin manager consumes the skill
content directly and does not interpret the catalog.

## Agents

Agents are specialist personas invoked automatically for multi-step tasks.

| Agent | Purpose |
|-------|---------|
| `grafana-debugger` | Delegates to `debug-with-grafana` for question-led diagnosis and evidence-backed conclusions |

## Plugin Structure

```
claude-plugin/
├── .claude-plugin/
│   └── plugin.json           # Plugin manifest
├── agents/
│   └── grafana-debugger.md   # Claude-specific specialist agent
├── skills-catalog.yaml      # Release lifecycle metadata, including retired names
└── skills/
    ├── <skill-name>/
    │   ├── SKILL.md
    │   └── references/...    # Optional skill-specific docs
    └── ...                   # Canonical portable gcx skill bundle
```

## Example Conversations

**Debugging a production incident:**
> "Latency on the checkout service spiked 10 minutes ago. Debug it."

Claude will invoke `grafana-debugger` and follow `debug-with-grafana`: scope the
incident with existing metrics, locate a representative anomalous trace, and
compare qualified baseline candidates with `gcx traces diff` when supported.
The differences direct targeted log, resource, or deployment checks. Conclusions
include evidence links and distinguish localized changes from proven causes.
Missing signals do not block an otherwise answerable question.

**Comparing a supplied trace:**
> "What changed in this slow trace compared with normal requests?"

The skill inspects the supplied trace directly, uses `gcx traces baseline` to
retrieve comparison candidates, qualifies them, and diffs baseline A against
anomalous B. It checks repeatability when needed and reports sampling, partiality,
or unavailable comparison capabilities rather than assuming a guaranteed RCA.

**Dashboard creation workflow:**
> "Create a checkout service triage dashboard in the SRE folder."

Claude will invoke `create-dashboard`, verify the target context/folder,
discover datasources and metric labels, author the dashboard, push it, render a
PNG with `gcx dashboards snapshot`, inspect the image, and iterate on layout or
queries before reporting the result.

**Dashboard GitOps workflow:**
> "Pull all dashboards from staging, validate them, and push to production."

Claude will invoke `manage-dashboards`, pull from the staging context, run
`gcx resources validate`, dry-run the push, and then apply to
production — with folder ordering handled automatically.

**Exploring what data exists:**
> "What Prometheus metrics are available for the payments service?"

Claude will use gcx datasource commands (`gcx datasources list`,
`gcx datasources prometheus labels`) to list metrics, filter by relevant
label selectors, and return sample queries you can use immediately.
