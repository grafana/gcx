## gcx irm oncall routes update-position

Update the position of a route in its integration.

### Synopsis

Update the position of a route in its integration.

Routes match from the top down, and the first match wins, so the position
decides which route handles an alert. The position is zero-based: 0 is the
first route. The backend renumbers the other routes of the integration.

The position field of the spec behaves differently on create: the backend
reads it as an insertion point, and it moves the route that holds that
position, and every later route, one place down. Use this command to set a
known index.

```
gcx irm oncall routes update-position <id> [flags]
```

### Options

```
  -h, --help            help for update-position
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, text, yaml (default "text")
      --position int    Zero-based target position (required)
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

* [gcx irm oncall routes](gcx_irm_oncall_routes.md)	 - Manage OnCall routes.

