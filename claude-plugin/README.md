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

## Repository Contract

`claude-plugin/skills/` is the current canonical portable Agent Skills bundle
for gcx. The Claude plugin consumes that tree directly today, and future
generic `.agents` installers should read from the same source rather than
forking or duplicating skill content elsewhere in the repository.

Claude-specific packaging remains under:

- `.claude-plugin/` — plugin manifest and marketplace metadata
- `agents/` — Claude-facing specialist personas

Do not add distributable gcx skills under repo-local `.agents/skills/`. Tools
that follow the `.agents` convention treat that path as repo-context guidance
for working on this repository, not as a globally installable skill bundle.

## Skills

Skills are triggered automatically when you describe what you want. You do not
need to invoke them by name. The table below is the current inventory of the
canonical portable skill bundle under `claude-plugin/skills/`.

| Skill | Purpose |
|-------|---------|
| `setup-gcx` | Install gcx if needed, configure authentication, and verify connectivity to Grafana |
| `gcx` | Use gcx as the default control plane for Grafana resources and queries |
| `scaffold-project` | Scaffold a new gcx resources-as-code project |
| `generate-resource-stubs` | Generate typed Grafana resource stubs as Go code |
| `import-dashboards` | Import existing Grafana dashboards into Go builder code |
| `manage-dashboards` | Pull, validate, create, push, and promote dashboards |
| `explore-datasources` | Discover datasources, metrics, labels, and log streams |
| `investigate-alert` | Investigate why a Grafana alert is firing and what it impacts |
| `debug-with-grafana` | Run a structured diagnostic workflow across metrics, logs, and dashboards |
| `slo-check-status` | Check SLO health and summarize current status |
| `slo-investigate` | Diagnose why a specific SLO is breaching or alerting |
| `slo-manage` | Create, update, pull, push, and delete SLO definitions |
| `slo-optimize` | Analyze SLO trends and recommend objective or alerting improvements |
| `synth-check-status` | Check Synthetic Monitoring health, status, and trends |
| `synth-investigate-check` | Diagnose why a Synthetic Monitoring check is failing |
| `synth-manage-checks` | Create, update, pull, push, and delete Synthetic Monitoring checks |
| `gcx-observability` | Roll out end-to-end observability: instrumentation, SLOs, alerts, synth, k6, IRM, dashboards, and cost optimization |

## Agents

Agents are specialist personas invoked automatically for multi-step tasks.

| Agent | Purpose |
|-------|---------|
| `grafana-debugger` | Autonomous debugging specialist — runs the full diagnostic workflow, correlates signals across datasources, and produces a root-cause report |

## Plugin Structure

```
claude-plugin/
├── .claude-plugin/
│   └── plugin.json           # Plugin manifest
├── agents/
│   └── grafana-debugger.md   # Claude-specific specialist agent
└── skills/
    ├── <skill-name>/
    │   ├── SKILL.md
    │   └── references/...    # Optional skill-specific docs
    └── ...                   # Canonical portable gcx skill bundle
```

## Example Conversations

**Debugging a production incident:**
> "Latency on the checkout service spiked 10 minutes ago. Debug it."

Claude will invoke `grafana-debugger`, run the `debug-with-grafana` skill,
query Prometheus for latency metrics, correlate with Loki error logs, and
return a root-cause analysis with the exact query commands used.

**Dashboard GitOps workflow:**
> "Pull all dashboards from staging, validate them, and push to production."

Claude will invoke `manage-dashboards`, pull from the staging context, run
`gcx resources validate`, dry-run the push, and then apply to
production — with folder ordering handled automatically.

**Exploring what data exists:**
> "What Prometheus metrics are available for the payments service?"

Claude will use `explore-datasources` to list metrics, filter by relevant
label selectors, and return sample queries you can use immediately.
