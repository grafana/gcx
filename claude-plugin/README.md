# gcx Claude Code Plugin

A Claude Code plugin that gives AI agents deep knowledge of gcx — the
kubectl-style CLI for managing Grafana resources. With this plugin, Claude can
debug Grafana incidents, explore datasources, manage dashboards, and drive full
GitOps workflows without hand-holding.

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

Skills are triggered automatically when you describe what you want. You do not
need to invoke them by name.

| Skill | Trigger phrases | What it does |
|-------|----------------|--------------|
| `setup-gcx` | "set up gcx", "configure gcx" | Install, authenticate, and verify gcx |
| `explore-datasources` | "what datasources exist", "explore metrics", "find log streams" | Discover Prometheus metrics, Loki log streams, labels, and series |
| `investigate-alert` | "why is this alert firing", "investigate alert X" | Root-cause an alert using metrics, logs, and correlated signals |
| `debug-with-grafana` | "debug this service", "diagnose latency", "troubleshoot errors" | 7-step diagnostic workflow: datasource → query → correlate → conclude |
| `manage-dashboards` | "pull dashboards", "push to Grafana", "promote to production" | Full dashboard lifecycle: pull, push, create, validate, promote |
| `gcx-observability` | "set up observability", "instrument my app", "observability setup" | End-to-end observability: instrumentation, SLOs, alerting, synthetic checks, k6, IRM, dashboards, cost optimization, GitOps export |

## Agents

Agents are specialist personas invoked automatically for multi-step tasks.

| Agent | Purpose |
|-------|---------|
| `grafana-debugger` | Autonomous debugging specialist — runs the full diagnostic workflow, correlates signals across datasources, and produces a root-cause report |

## Plugin Structure

```
claude-plugin/
├── .claude-plugin/
│   └── plugin.json                    # Plugin manifest
├── agents/
│   └── grafana-debugger.md            # Debugging specialist agent
└── skills/
    ├── setup-gcx/
    │   ├── SKILL.md
    │   └── references/configuration.md
    ├── explore-datasources/
    │   ├── SKILL.md
    │   └── references/
    │       ├── discovery-patterns.md
    │       └── logql-syntax.md
    ├── investigate-alert/
    │   ├── SKILL.md
    │   └── references/alert-investigation-patterns.md
    ├── debug-with-grafana/
    │   ├── SKILL.md
    │   └── references/
    │       ├── error-recovery.md
    │       └── query-patterns.md
    ├── manage-dashboards/
    │   ├── SKILL.md
    │   └── references/
    │       ├── resource-operations.md
    │       └── resource-model.md
    └── gcx-observability/
        └── SKILL.md
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
