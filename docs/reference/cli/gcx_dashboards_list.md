## gcx dashboards list

List dashboards

```
gcx dashboards list [flags]
```

### Options

```
      --api-version string   API version to use (e.g. dashboard.grafana.app/v1); defaults to server preferred version
      --continue string      Continue token for the next page (requires --limit > 0; use the token shown by a previous limited response)
  -h, --help                 help for list
      --jq string            jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int            Maximum number of dashboards to return in one page (0 fetches all pages) (default 50)
  -o, --output string        Output format. One of: agents, json, table, wide, yaml (default "table")
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

* [gcx dashboards](gcx_dashboards.md)	 - Manage Grafana dashboards

