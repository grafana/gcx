## gcx datasources influxdb query

Execute a query against an InfluxDB datasource

### Synopsis

Execute a query against an InfluxDB datasource.

EXPR is the InfluxQL, Flux, or SQL expression to evaluate, passed as a positional
argument or via --expr.
The query language mode is auto-detected from the datasource configuration.
Datasource is resolved from -d flag or datasources.influxdb in your context.

```
gcx datasources influxdb query [EXPR] [flags]
```

### Examples

```

  # InfluxQL instant query
  gcx datasources influxdb query -d UID 'SELECT mean("value") FROM "cpu" WHERE time > now() - 1h'

  # InfluxQL range query
  gcx datasources influxdb query -d UID 'SELECT mean("value") FROM "cpu"' --from now-1h --to now

  # Output as JSON
  gcx datasources influxdb query -d UID 'SELECT * FROM "cpu" LIMIT 10' -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.influxdb is configured)
      --expr string         Query expression (alternative to positional argument)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for query
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: json, table, wide, yaml (default "table")
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

* [gcx datasources influxdb](gcx_datasources_influxdb.md)	 - Query InfluxDB datasources

