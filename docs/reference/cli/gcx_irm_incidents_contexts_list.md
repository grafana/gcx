## gcx irm incidents contexts list

List contexts attached to an incident.

```
gcx irm incidents contexts list <incident-id> [flags]
```

### Options

```
      --alert-group-id string   Filter by linked alert group ID
  -h, --help                    help for list
      --json string             Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int               Maximum number of contexts to return (0 = server default)
  -o, --output string           Output format. One of: agents, json, table, wide, yaml (default "table")
      --status string           Filter by context status
      --type string             Filter by context type (e.g. genericURL, grafana.dashboard, code.github.pr). Note: alert-group links are encoded as genericURL contexts with alertGroupID set — use --alert-group-id to filter those.
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

* [gcx irm incidents contexts](gcx_irm_incidents_contexts.md)	 - Manage incident contexts (linked alert groups, dashboards, etc.).

