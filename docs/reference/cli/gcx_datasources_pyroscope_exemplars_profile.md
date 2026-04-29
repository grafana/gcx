## gcx datasources pyroscope exemplars profile

List individual profile exemplars

### Synopsis

List individual profile exemplars by calling SelectSeries with EXEMPLAR_TYPE_INDIVIDUAL.

Each row is a concrete profile sample identified by Profile ID. When profiles are
span-aware (e.g. via otelpyroscope), a Span ID column is included linking to the
associated trace span.

EXPR is the label selector (e.g. '{service_name="frontend"}').

```
gcx datasources pyroscope exemplars profile [EXPR] [flags]
```

### Examples

```

  # Top profile exemplars in the last hour
  gcx datasources pyroscope exemplars profile '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h

  # JSON output
  gcx datasources pyroscope exemplars profile '{}' --since 30m -o json
```

### Options

```
  -d, --datasource string       Datasource UID (required unless datasources.pyroscope is configured)
      --expr string             Label selector (alternative to positional argument)
      --from string             Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                    help for profile
      --json string             Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --max-label-columns int   Max label columns in table output (0 hides label columns) (default 3)
  -o, --output string           Output format. One of: json, table, yaml (default "table")
      --profile-type string     Profile type ID (default "process_cpu:cpu:nanoseconds:cpu:nanoseconds")
      --since string            Duration before --to (or now if omitted); mutually exclusive with --from
      --to string               End time (RFC3339, Unix timestamp, or relative like 'now')
      --top-n int               Maximum number of exemplars to return (default 100)
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

* [gcx datasources pyroscope exemplars](gcx_datasources_pyroscope_exemplars.md)	 - Query profile or span exemplars from a Pyroscope datasource

