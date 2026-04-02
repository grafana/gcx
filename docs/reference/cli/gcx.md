## gcx

Control plane for Grafana Cloud operations

### Synopsis

gcx is a unified CLI for managing Grafana resources, dashboards, datasources, alerting, and Cloud product APIs (SLO, OnCall, Synthetic Monitoring, Fleet, K6, and more).

### Options

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --context string   Name of the context to use (overrides current-context in config)
  -h, --help             help for gcx
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx alert](gcx_alert.md)	 - Manage Grafana alert rules and alert groups
* [gcx api](gcx_api.md)	 - Make direct HTTP requests to the Grafana API
* [gcx appo11y](gcx_appo11y.md)	 - Manage Grafana App Observability settings
* [gcx auth](gcx_auth.md)	 - Manage authentication
* [gcx commands](gcx_commands.md)	 - List all commands with rich metadata for agent consumption
* [gcx config](gcx_config.md)	 - View or manipulate configuration settings
* [gcx dashboards](gcx_dashboards.md)	 - Render Grafana dashboard snapshots
* [gcx datasources](gcx_datasources.md)	 - Manage and query Grafana datasources
* [gcx dev](gcx_dev.md)	 - Manage Grafana resources as code
* [gcx fleet](gcx_fleet.md)	 - Manage Grafana Fleet Management pipelines and collectors
* [gcx help-tree](gcx_help-tree.md)	 - Print a compact command tree for agent context injection
* [gcx incidents](gcx_incidents.md)	 - Manage Grafana Incident Response and Management (IRM) incidents
* [gcx k6](gcx_k6.md)	 - Manage Grafana K6 Cloud projects, load tests, and schedules
* [gcx kg](gcx_kg.md)	 - Manage Grafana Knowledge Graph entity types, rules, and datasets
* [gcx logs](gcx_logs.md)	 - Query Loki datasources and manage Adaptive Logs
* [gcx metrics](gcx_metrics.md)	 - Query Prometheus datasources and manage Adaptive Metrics
* [gcx oncall](gcx_oncall.md)	 - Manage Grafana OnCall integrations, escalation chains, schedules, and routing
* [gcx profiles](gcx_profiles.md)	 - Query Pyroscope datasources and manage continuous profiling
* [gcx providers](gcx_providers.md)	 - Manage registered providers
* [gcx resources](gcx_resources.md)	 - Manipulate Grafana resources
* [gcx setup](gcx_setup.md)	 - Onboard and configure Grafana Cloud products.
* [gcx slo](gcx_slo.md)	 - Manage Grafana SLO definitions and reports
* [gcx synth](gcx_synth.md)	 - Manage Grafana Synthetic Monitoring checks and probes
* [gcx traces](gcx_traces.md)	 - Query Tempo datasources and manage Adaptive Traces

