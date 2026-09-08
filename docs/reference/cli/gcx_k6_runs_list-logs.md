## gcx k6 runs list-logs

List logs for a k6 test run.

### Synopsis

List logs for one k6 test run. The optional LogQL pipeline must start with '|'. The command always limits the query to the selected run.

```
gcx k6 runs list-logs <run-id> [PIPELINE] [flags]
```

### Examples

```
  gcx k6 runs list-logs 12345
  gcx k6 runs list-logs 12345 '|= `error`' --since 15m
  gcx k6 runs list-logs 12345 --direction forward --limit 100 -o raw
```

### Options

```
      --direction string   Read logs forward or backward (default "backward")
      --from string        Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help               help for list-logs
      --jq string          jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string        Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int          Maximum number of log entries (default 50)
  -o, --output string      Output format. One of: agents, json, raw, table, wide, yaml (default "table")
      --since string       Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --to string          End time (RFC3339, Unix timestamp, or relative like 'now')
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

* [gcx k6 runs](gcx_k6_runs.md)	 - Manage k6 test runs.

