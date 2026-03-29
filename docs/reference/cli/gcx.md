## gcx

Control plane for Grafana Cloud operations

### Synopsis

gcx is a unified CLI for managing Grafana resources, dashboards, datasources, alerting, and Cloud product APIs (SLO, OnCall, Synthetic Monitoring, Fleet, K6, and more).

### Options

```
      --agent           Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
  -h, --help            help for gcx
      --no-color        Disable color output
      --no-truncate     Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count   Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx adaptive](gcx_adaptive.md)	 - Manage Grafana Cloud Adaptive Telemetry.
* [gcx alert](gcx_alert.md)	 - Manage Grafana alert rules and alert groups
* [gcx api](gcx_api.md)	 - Make direct HTTP requests to the Grafana API
* [gcx commands](gcx_commands.md)	 - List all commands with rich metadata for agent consumption
* [gcx config](gcx_config.md)	 - View or manipulate configuration settings
* [gcx dashboards](gcx_dashboards.md)	 - Render Grafana dashboard snapshots
* [gcx datasources](gcx_datasources.md)	 - Manage and query Grafana datasources
* [gcx dev](gcx_dev.md)	 - Manage Grafana resources as code
* [gcx fleet](gcx_fleet.md)	 - Manage Grafana Fleet Management pipelines and collectors
* [gcx incidents](gcx_incidents.md)	 - Manage Grafana Incident Response and Management (IRM) incidents
* [gcx k6](gcx_k6.md)	 - Manage Grafana K6 Cloud projects, load tests, and schedules
* [gcx kg](gcx_kg.md)	 - Manage Grafana Knowledge Graph entity types, rules, and datasets
* [gcx oncall](gcx_oncall.md)	 - Manage Grafana OnCall integrations, escalation chains, schedules, and routing
* [gcx providers](gcx_providers.md)	 - List registered providers
* [gcx resources](gcx_resources.md)	 - Manipulate Grafana resources
* [gcx slo](gcx_slo.md)	 - Manage Grafana SLO definitions and reports
* [gcx synth](gcx_synth.md)	 - Manage Grafana Synthetic Monitoring checks and probes

