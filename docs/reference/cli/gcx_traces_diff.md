## gcx traces diff

[experimental] Compare two traces (baseline vs comparison)

### Synopsis

[experimental] Compare two traces using the Tempo trace-diff API.

This is an experimental, Grafana Cloud-only endpoint: it may be unavailable on
self-hosted or OSS Tempo, and its request/response shape may change.

TRACE_A is the baseline trace and TRACE_B is the comparison trace. Deltas use
B - A semantics: negative means B is faster (improvement), positive means B is
slower (regression).

Datasource is resolved from the -d flag or datasources.tempo in your context.

Use --since or --from/--to to bound the lookup: narrowing the window helps
Tempo locate older traces faster. When omitted, the datasource performs a full
lookback.

```
gcx traces diff TRACE_A TRACE_B [flags]
```

### Examples

```

  # Compare two traces (B - A semantics); experimental, Grafana Cloud-only
  gcx traces diff <trace-a> <trace-b>

  # With an explicit datasource UID, JSON output
  gcx traces diff -d UID <trace-a> <trace-b> -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.tempo is configured)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for diff
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, json, yaml (default "json")
      --since string        Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx traces](gcx_traces.md)	 - Query Tempo datasources and manage Adaptive Traces

