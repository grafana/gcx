## gcx synthetic-monitoring query

Run a Synthetic Monitoring query by name.

### Synopsis

Run a query the Synthetic Monitoring backend knows by name.

The backend owns the expression and picks the datasource that holds the data, so
no PromQL or LogQL is sent or required. Parameters are passed with -p and are
validated by the backend, which reports the expression it ran.

```
gcx synthetic-monitoring query NAME [flags]
```

### Examples

```

  # Uptime for a check, as the app computes it
  gcx synthetic-monitoring query checks_uptime \
    -p job=my-check -p instance=https://example.com -p frequency=60000

  # Reachability over the last day
  gcx synthetic-monitoring query reachability \
    -p job=my-check -p instance=https://example.com -p frequency=60000 --from now-1d
```

### Options

```
      --from string         Start of the time range (default "now-3h")
  -h, --help                help for query
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, json, table, yaml (default "table")
  -p, --param stringArray   Query parameter as key=value (repeatable), e.g. -p job=my-check
      --to string           End of the time range (default "now")
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

* [gcx synthetic-monitoring](gcx_synthetic-monitoring.md)	 - Manage Grafana Synthetic Monitoring checks and probes

