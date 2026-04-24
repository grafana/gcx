## gcx metrics billing query

Execute a PromQL query against billing metrics

### Synopsis

Execute a PromQL query against a Prometheus datasource.

EXPR is the PromQL expression to evaluate, passed as a positional argument or
via --expr (familiar to promtool users).
Datasource is resolved from -d flag or datasources.prometheus in your context.
Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds.

```
gcx metrics billing query [EXPR] [flags]
```

### Examples

```

  # Active series right now
  gcx metrics billing query 'grafanacloud_instance_active_series'

  # Active series over the last hour
  gcx metrics billing query 'grafanacloud_instance_active_series' --since 1h --step 1m

  # Print a Grafana Explore share link for the executed query
  gcx metrics billing query 'grafanacloud_instance_active_series' --share-link

  # Output as JSON
  gcx metrics billing query 'grafanacloud_instance_active_series' -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.prometheus is configured)
      --expr string         Query expression (alternative to positional argument)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for query
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --open                Open the executed query in Grafana Explore
  -o, --output string       Output format. One of: graph, json, table, wide, yaml (default "table")
      --share-link          Print the Grafana Explore URL for the executed query to stderr
      --since string        Duration before --to (or now if omitted); mutually exclusive with --from
      --step string         Query step (e.g., '15s', '1m')
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx metrics billing](gcx_metrics_billing.md)	 - Query Grafana Cloud billing metrics (grafanacloud_*)

