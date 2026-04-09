## gcx profiles metrics

Query profile time-series data from a Pyroscope datasource

### Synopsis

Query profile time-series data via SelectSeries from a Pyroscope datasource.

Shows how a profile metric (e.g., CPU, memory) changes over time. Useful for
identifying performance regressions and trends before diving into flamegraphs.

Use --top to aggregate the time range into a ranked leaderboard of the heaviest
consumers (equivalent to profilecli query top). Without --top, returns raw
time-series data points for trend analysis.

EXPR is the label selector (e.g., '{service_name="frontend"}').
Datasource is resolved from -d flag or datasources.pyroscope in your context.

```
gcx profiles metrics EXPR [flags]
```

### Examples

```

  # Top services by CPU usage (ranked leaderboard)
  gcx profiles metrics '{}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h --top

  # CPU usage over the last hour with 1-minute resolution
  gcx profiles metrics -d pyro-001 '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h --step 1m

  # Output as JSON
  gcx profiles metrics -d abc123 '{}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h --top -o json
```

### Options

```
      --aggregation string    Aggregation type: 'sum' or 'average'
  -d, --datasource string     Datasource UID (required unless datasources.pyroscope is configured)
      --from string           Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
      --group-by strings      Group series by label (repeatable, defaults to service_name)
  -h, --help                  help for metrics
      --json string           Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int             Maximum number of series to return (default 10)
  -o, --output string         Output format. One of: graph, json, table, wide, yaml (default "table")
      --profile-type string   Profile type ID (e.g., 'process_cpu:cpu:nanoseconds:cpu:nanoseconds') (required)
      --since string          Duration before --to (or now if omitted); mutually exclusive with --from
      --step string           Query step (e.g., '15s', '1m')
      --to string             End time (RFC3339, Unix timestamp, or relative like 'now')
      --top                   Aggregate into a ranked leaderboard (equivalent to profilecli query top)
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

* [gcx profiles](gcx_profiles.md)	 - Query Pyroscope datasources and manage continuous profiling

