## gcx profiles query

Execute a profiling query against a Pyroscope datasource

### Synopsis

Execute a profiling query against a Pyroscope datasource.

DATASOURCE_UID is optional when datasources.pyroscope is configured in your context.
EXPR is the label selector (e.g., '{service_name="frontend"}').

```
gcx profiles query [DATASOURCE_UID] EXPR [flags]
```

### Examples

```

  # Query profiles using configured default datasource
  gcx profiles query <datasource-uid> 'process_cpu:cpu:nanoseconds:cpu:nanoseconds{}'

  # Output as JSON
  gcx profiles query <datasource-uid> '<expr>' -o json
```

### Options

```
      --from string           Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                  help for query
      --json string           Comma-separated list of fields to include in JSON output, or '?' to discover available fields
      --max-nodes int         Maximum nodes in flame graph (default 1024)
  -o, --output string         Output format. One of: graph, json, table, wide, yaml (default "table")
      --profile-type string   Profile type ID (e.g., 'process_cpu:cpu:nanoseconds:cpu:nanoseconds') (required)
      --step string           Query step (e.g., '15s', '1m')
      --to string             End time (RFC3339, Unix timestamp, or relative like 'now')
      --window string         Convenience shorthand: sets --from to now-{window} and --to to now (mutually exclusive with --from/--to)
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx profiles](gcx_profiles.md)	 - Query Pyroscope datasources and manage continuous profiling

