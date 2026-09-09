## gcx alert state-history list

List recorded alert state transitions.

```
gcx alert state-history list [flags]
```

### Options

```
      --from string         Start of the time range (RFC3339, Unix seconds, or relative e.g. now-6h) (default "now-6h")
  -h, --help                help for list
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of dotted field paths to include in JSON output (e.g. spec.name), or 'list' (or '?') to discover the available paths
      --label stringArray   Filter by instance label equality (key=value); repeatable
      --limit int           Maximum number of records to return (0 for the backend default) (default 100)
  -o, --output string       Output format. One of: agents, json, table, wide, yaml (default "table")
      --rule string         Filter by rule UID (required by the annotations history backend)
      --to string           End of the time range (RFC3339, Unix seconds, or relative e.g. now) (default "now")
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

* [gcx alert state-history](gcx_alert_state-history.md)	 - Inspect alert state history.

