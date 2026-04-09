## gcx datasources query

Execute a query against any datasource (auto-detects type)

### Synopsis

Execute a query against any datasource, automatically detecting the datasource type.

DATASOURCE_UID is always required (no default resolution for generic).
EXPR is the query expression appropriate for the datasource type.

The datasource type is detected via the Grafana API and the appropriate query
client is used automatically. This is the escape hatch for datasource types
that do not have a dedicated subcommand.

```
gcx datasources query DATASOURCE_UID EXPR [flags]
```

### Examples

```

  # Auto-detect and query any supported datasource
  gcx datasources query ds-001 'up{job="grafana"}' --from now-1h --to now

  # Loki via auto-detect (with limit)
  gcx datasources query loki-001 '{job="varlogs"}' --from now-1h --to now --limit 200

  # Pyroscope via auto-detect
  gcx datasources query pyro-001 '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --from now-1h --to now
```

### Options

```
      --from string           Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                  help for query
      --json string           Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int             Maximum number of log lines to return for loki queries (0 means no limit) (default 50)
      --max-nodes int         Maximum nodes in flame graph (pyroscope only) (default 1024)
  -o, --output string         Output format. One of: graph, json, table, wide, yaml (default "table")
      --profile-type string   Profile type ID for pyroscope queries (e.g., 'process_cpu:cpu:nanoseconds:cpu:nanoseconds')
      --since string          Duration before --to (or now if omitted); mutually exclusive with --from
      --step string           Query step (e.g., '15s', '1m')
      --to string             End time (RFC3339, Unix timestamp, or relative like 'now')
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

* [gcx datasources](gcx_datasources.md)	 - Manage and query Grafana datasources

