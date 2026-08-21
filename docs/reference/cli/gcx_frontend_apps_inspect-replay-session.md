## gcx frontend apps inspect-replay-session

Inspect replay recordings attached to a Frontend Observability session ID.

```
gcx frontend apps inspect-replay-session <app-name> <session-id> [flags]
```

### Examples

```
  # Inspect replay recordings for a regular session ID.
  gcx frontend apps inspect-replay-session my-web-app-42 abc-session-123

  # Inspect replay recordings with JSON output.
  gcx frontend apps inspect-replay-session my-web-app-42 abc-session-123 -o json

  # Inspect all replay recordings attached to a session ID.
  gcx frontend apps inspect-replay-session my-web-app-42 abc-session-123 --limit 0
```

### Options

```
  -h, --help            help for inspect-replay-session
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int       Maximum number of replay recordings to return (0 for unlimited) (default 50)
  -o, --output string   Output format. One of: agents, json, text, yaml (default "text")
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

* [gcx frontend apps](gcx_frontend_apps.md)	 - Manage Frontend Observability apps.

