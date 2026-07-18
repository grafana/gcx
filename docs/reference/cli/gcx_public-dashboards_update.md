## gcx public-dashboards update

Update a public dashboard config from a JSON file.

```
gcx public-dashboards update PD_UID [flags]
```

### Options

```
      --dashboard-uid string   Parent dashboard UID (required)
  -f, --file string            File containing the public dashboard spec (JSON), or '-' for stdin (required)
  -h, --help                   help for update
      --jq string              jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string            Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string          Output format. One of: agents, json, table, yaml (default "table")
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

* [gcx public-dashboards](gcx_public-dashboards.md)	 - Manage public dashboards

