## gcx datasources loki query-patterns

Detect recurring log patterns

### Synopsis

Detect recurring log line patterns for a LogQL stream selector.

EXPR is the LogQL stream selector (e.g., '{job="varlogs"}'); Loki extracts the
stream selector from a full LogQL expression server-side, so a query/metrics
expression works here too.
Datasource is resolved from -d flag or datasources.loki in your context.
Requires the Loki server to have pattern_ingester enabled — returns no
patterns (not an error) otherwise.
Default time range is the last hour when no time flags are given.

```
gcx datasources loki query-patterns [EXPR] [flags]
```

### Examples

```

  # Detect patterns using configured default datasource
  gcx datasources loki query-patterns '{job="varlogs"}'

  # Detect patterns over a specific window
  gcx datasources loki query-patterns -d UID '{job="varlogs"}' --since 6h

  # Output as JSON
  gcx datasources loki query-patterns -d UID '{job="varlogs"}' -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.loki is configured)
      --expr string         Query expression (alternative to positional argument)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for query-patterns
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, json, table, wide, yaml (default "table")
      --since string        Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --step string         Query step (e.g., '15s', '1m')
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from known agent identity variables. Set GCX_AGENT_NAME to identify a supported harness, or GCX_AGENT_MODE to control the mode.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx datasources loki](gcx_datasources_loki.md)	 - Query Loki datasources

