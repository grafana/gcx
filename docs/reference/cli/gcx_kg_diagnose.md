## gcx kg diagnose

Run diagnostic checks on the Knowledge Graph pipeline.

### Synopsis

Run diagnostic checks to verify the Knowledge Graph is healthy.

Checks stack status, sanity results, entity counts, scope values,
telemetry drilldown configuration, and recording rule metrics in
Mimir. Use --env to scope checks to a specific environment.

Metric checks require a Prometheus datasource. The datasource UID is
resolved from --datasource, the datasources.prometheus config key, or
auto-discovery. If unavailable, metric checks are skipped.

```
gcx kg diagnose [flags]
```

### Examples

```
  gcx kg diagnose
  gcx kg diagnose --env production
  gcx kg diagnose --env staging --output json
  gcx kg diagnose --datasource grafanacloud-prom
```

### Options

```
  -d, --datasource string   Prometheus datasource UID (auto-discovered if omitted)
      --env string          Environment scope
  -h, --help                help for diagnose
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --namespace string    Namespace scope
  -o, --output string       Output format. One of: agents, json, table, yaml (default "table")
      --site string         Site scope
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx kg](gcx_kg.md)	 - Manage Grafana Knowledge Graph rules, entities, and insights
* [gcx kg diagnose labels](gcx_kg_diagnose_labels.md)	 - Validate the deployment_environment → asserts_env label pipeline.
* [gcx kg diagnose service](gcx_kg_diagnose_service.md)	 - Diagnose a specific service in the Knowledge Graph.

