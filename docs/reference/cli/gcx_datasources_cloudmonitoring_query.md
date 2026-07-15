## gcx datasources cloudmonitoring query

Execute a Google Cloud Monitoring metrics query

### Synopsis

Execute a Google Cloud Monitoring (formerly Stackdriver) metrics query.

Queries are structured (project, metric type, reducer, aligner) — there is no
expression language. Use --group-by to split the result into one series per
label value, and --filter to narrow by labels.

Use list-projects and list-metrics to discover valid flag values.
Datasource is resolved from -d flag or datasources.cloudmonitoring in your context.

```
gcx datasources cloudmonitoring query [flags]
```

### Examples

```

  # CPU utilization across a project
  gcx datasources cloudmonitoring query -d UID --project my-project \
    --metric compute.googleapis.com/instance/cpu/utilization --since 1h

  # Split by instance, mean-reduced
  gcx datasources cloudmonitoring query -d UID --project my-project \
    --metric compute.googleapis.com/instance/cpu/utilization \
    --reducer REDUCE_MEAN --group-by resource.label.instance_name --since 1h

  # Chart it
  gcx datasources cloudmonitoring query -d UID --project my-project \
    --metric compute.googleapis.com/instance/cpu/utilization --since 6h -o graph
```

### Options

```
      --aligner string            Per-series aligner: ALIGN_MEAN, ALIGN_SUM, ALIGN_MIN, ALIGN_MAX, ALIGN_RATE, ALIGN_DELTA, ... (default "ALIGN_MEAN")
      --alignment-period string   Alignment period, e.g. +60s (default: auto-fit the time range)
  -d, --datasource string         Datasource UID (required unless datasources.cloudmonitoring is configured)
      --filter stringToString     Label filter key=value (repeatable, e.g. --filter resource.label.zone=us-east1-b) (default [])
      --from string               Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
      --group-by stringArray      Label to split series by, e.g. resource.label.instance_name (repeatable)
  -h, --help                      help for query
      --jq string                 jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string               Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --metric string             Metric type, e.g. compute.googleapis.com/instance/cpu/utilization (required)
  -o, --output string             Output format. One of: agents, graph, json, table, wide, yaml (default "table")
      --project string            GCP project ID (required)
      --reducer string            Cross-series reducer: REDUCE_NONE, REDUCE_MEAN, REDUCE_SUM, REDUCE_MIN, REDUCE_MAX, REDUCE_COUNT, ... (default "REDUCE_NONE")
      --since string              Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --to string                 End time (RFC3339, Unix timestamp, or relative like 'now')
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

* [gcx datasources cloudmonitoring](gcx_datasources_cloudmonitoring.md)	 - Query Google Cloud Monitoring datasources

