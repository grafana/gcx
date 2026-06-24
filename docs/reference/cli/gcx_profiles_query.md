## gcx profiles query

Execute a profiling query against a Pyroscope datasource

### Synopsis

Execute a profiling query against a Pyroscope datasource.

EXPR is the label selector (e.g., '{service_name="frontend"}').
Datasource is resolved from -d flag or datasources.pyroscope in your context.

```
gcx profiles query [EXPR] [flags]
```

### Examples

```

  # Profile query with explicit datasource UID
  gcx profiles query -d abc123 '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h

  # Using configured default datasource
  gcx profiles query '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h

  # Output as JSON
  gcx profiles query -d abc123 '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds -o json

  # Drill into one or more specific profiles found via 'gcx profiles exemplars'
  # (--profile-id is repeatable; pass it once per UUID)
  gcx profiles query '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h \
    --profile-id 550e8400-e29b-41d4-a716-446655440000 \
    --profile-id 7c9e6679-7425-40de-944b-e07fc1f90ae7

  # Restrict the flamegraph to stacks rooted at a specific call site
  # (--stacktrace-selector is repeatable; pass it once per frame, root first)
  gcx profiles query '{service_name="my-go-service"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h \
    --stacktrace-selector 'github.com/prometheus/client_golang/prometheus.(*Registry).Gather.func1'
```

### Options

```
  -d, --datasource string             Datasource UID (required unless datasources.pyroscope is configured)
      --expr string                   Query expression (alternative to positional argument)
      --from string                   Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                          help for query
      --jq string                     jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string                   Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --max-nodes int                 Maximum nodes in flame graph (default 0/unlimited for pprof output, 50000 for all other formats)
  -o, --output string                 Output format. One of: agents, graph, json, pprof, table, wide, yaml (default "table")
      --pprof-overwrite               Overwrite the output file if it already exists (only with -o pprof)
      --pprof-path string             Destination path for pprof binary output (only with -o pprof; default: profile-YYYY-MM-DD-HHMMSS.pb.gz)
      --profile-id strings            Drill down to specific profile UUIDs from exemplar queries (repeatable)
      --profile-type string           Profile type ID (e.g., 'process_cpu:cpu:nanoseconds:cpu:nanoseconds'); use 'gcx profiles profile-types' to list available (required)
      --since string                  Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --stacktrace-selector strings   Only query locations with these function names, starting from the root (repeatable)
      --step string                   Query step (e.g., '15s', '1m')
      --to string                     End time (RFC3339, Unix timestamp, or relative like 'now')
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

* [gcx profiles](gcx_profiles.md)	 - Query Pyroscope datasources and manage continuous profiling

