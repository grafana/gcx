## gcx dbo11y instances list

List Database Observability instances discovered from telemetry.

### Synopsis

List the database instances Grafana Cloud Database Observability has discovered.

Discovery uses the database_observability_connection_info inventory metric
emitted by the database_observability.postgres Alloy component (job
"integrations/db-o11y"): one row per monitored database instance, with engine
and cloud-provider metadata.

Related: "gcx dbo11y instances get <name>" drills into one instance's health,
connections, wait events, and top queries by time share.

When no instances are found, this command checks whether Database
Observability has been activated for the stack and, if not, exits non-zero
with a hint to activate it in Grafana Cloud instead of a generic empty
result.

```
gcx dbo11y instances list [flags]
```

### Examples

```

  # List all database instances in the current stack
  gcx dbo11y instances list

  # Filter to Postgres instances
  gcx dbo11y instances list --filter engine=postgres

  # Pin a datasource and output JSON
  gcx dbo11y instances list -d grafanacloud-prom -o json
```

### Options

```
  -d, --datasource string    Prometheus datasource UID (defaults to datasources.prometheus in config or auto-discovery)
      --filter stringArray   Restrict to instances matching a label matcher, e.g. --filter engine=postgres (repeatable)
  -h, --help                 help for list
      --jq string            jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int            Limit the number of instances returned (0 = unlimited) (default 50)
  -o, --output string        Output format. One of: agents, json, table, wide, yaml (default "table")
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

* [gcx dbo11y instances](gcx_dbo11y_instances.md)	 - Inspect Database Observability instances discovered from telemetry

