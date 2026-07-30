## gcx datasources pyroscope stats

Show ingestion stats (data ingested, oldest/newest profile times)

### Synopsis

Show ingestion stats for a Pyroscope datasource: whether any profiling data was ever ingested, and the oldest and newest profile times.

Use this to disambiguate empty query results: if no data was ever ingested, fix ingestion before adjusting selectors; otherwise the oldest/newest bounds show the actual queryable time window.

If gcx auto-discovers the datasource from your Grafana Cloud stack, the discovered datasource UID may be saved to your gcx configuration for future commands.

```
gcx datasources pyroscope stats [flags]
```

### Examples

```

	# Check whether the datasource is receiving profiling data
	gcx datasources pyroscope stats -d UID

	# Output as JSON (times are milliseconds since epoch)
	gcx datasources pyroscope stats -d UID -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.pyroscope is configured)
  -h, --help                help for stats
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, json, table, yaml (default "table")
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

* [gcx datasources pyroscope](gcx_datasources_pyroscope.md)	 - Query Pyroscope datasources

