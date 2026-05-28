## gcx appo11y services list

List services discovered from target_info telemetry

### Synopsis

List the services Grafana Cloud Application Observability has discovered from telemetry.

Discovery uses the same approach as the App Observability plugin: a group-by
query against the OTel target_info metric, projected onto (job, telemetry_sdk_language).
Each result row is one service.

```
gcx appo11y services list [flags]
```

### Examples

```

  # List all services in the current stack
  gcx appo11y services list

  # Filter to Go services running in production
  gcx appo11y services list --language go --env production

  # Show the top 20 services with extra target_info labels
  gcx appo11y services list --limit 20 --columns service_version,k8s_pod_name

  # Per-language summary instead of the full list
  gcx appo11y services list --count

  # Pin a datasource and output JSON
  gcx appo11y services list -d grafanacloud-prom -o json
```

### Options

```
      --columns strings             Extra target_info labels to surface as table columns (comma-separated)
      --count                       Print a per-language summary instead of the full list
  -d, --datasource string           Prometheus datasource UID (defaults to datasources.prometheus in config or auto-discovery)
      --env string                  Restrict to a single deployment_environment (e.g. production)
      --filter stringArray          Restrict to services matching a label matcher, e.g. --filter k8s_namespace_name=prod (repeatable)
  -h, --help                        help for list
      --json string                 Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --language string             Restrict to a single telemetry_sdk_language (e.g. go, java, nodejs)
      --limit int                   Limit the number of services returned (0 = unlimited; applied after sorting)
  -o, --output string               Output format. One of: agents, json, table, wide, yaml (default "table")
      --target-info-metric string   Override the inventory metric (advanced; mirrors the plugin's metricName:targetInfo variable) (default "target_info")
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use (overrides current-context in config)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx appo11y services](gcx_appo11y_services.md)	 - Inspect Application Observability services discovered from telemetry

