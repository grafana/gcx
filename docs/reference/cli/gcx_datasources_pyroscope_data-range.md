## gcx datasources pyroscope data-range

Show the range of profiling data the datasource holds

### Synopsis

Show the range of profiling data a Pyroscope datasource holds: whether any data was ever ingested, and the oldest and newest profile times. The range covers everything behind the datasource (the whole tenant), not any particular service or label selector.

Use this to disambiguate empty query results: if no data was ever ingested, fix ingestion before adjusting selectors; otherwise the oldest/newest bounds show the currently queryable window.

Note that data-ingested is a lifetime flag: it stays true even after all data has aged out of the retention period (31 days by default, tenant-configurable), in which case the bounds render as '-' (0 in JSON). Older backends may also report an unknown oldest bound the same way.

If gcx auto-discovers the datasource from your Grafana Cloud stack, the discovered datasource UID may be saved to your gcx configuration for future commands.

```
gcx datasources pyroscope data-range [flags]
```

### Examples

```

	# Check whether the datasource holds profiling data, and for what range
	gcx datasources pyroscope data-range -d UID

	# Output as JSON (times are milliseconds since epoch)
	gcx datasources pyroscope data-range -d UID -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.pyroscope is configured)
  -h, --help                help for data-range
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of dotted field paths to include in JSON output (e.g. spec.name), or 'list' (or '?') to discover the available paths
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

