## gcx datasources pyroscope query-anomalies

[experimental] Query profile anomalies from a Pyroscope datasource

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

Query profiles flagged as anomalies by an external anomaly source and
confirmed present in ingested data for the given label selector and time range.

Requires the datasource's query-frontend to have an anomaly source configured
(query-frontend.anomaly-api.url); returns FAILED_PRECONDITION otherwise.

EXPR is the label selector (e.g. '{service_name="frontend"}'). It may resolve
to more than one service_name; anomalies from every matching service are
queried and confirmed in one call.

```
gcx datasources pyroscope query-anomalies [EXPR] [flags]
```

### Examples

```

  # Anomalies for a service in the last hour
  gcx datasources pyroscope query-anomalies -d UID '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h

  # Every service in a namespace
  gcx datasources pyroscope query-anomalies -d UID '{namespace="prod"}' --since 1h

  # JSON output
  gcx datasources pyroscope query-anomalies -d UID '{service_name="frontend"}' --since 1h -o json
```

### Options

```
      --anomaly-type strings    Anomaly source(s) to query. Only 'stacktrace' is supported today. Repeatable (default [stacktrace])
  -d, --datasource string       Datasource UID (required unless datasources.pyroscope is configured)
      --expr string             Label selector (alternative to positional argument)
      --from string             Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                    help for query-anomalies
      --jq string               jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string             Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --max-label-columns int   Max label columns in table output (0 hides label columns) (default 3)
  -o, --output string           Output format. One of: agents, json, table, yaml (default "table")
      --profile-type string     Profile type ID (default "process_cpu:cpu:nanoseconds:cpu:nanoseconds")
      --since string            Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --to string               End time (RFC3339, Unix timestamp, or relative like 'now')
      --top-n int               Maximum number of anomalies to return (default 100)
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

* [gcx datasources pyroscope](gcx_datasources_pyroscope.md)	 - Query Pyroscope datasources

