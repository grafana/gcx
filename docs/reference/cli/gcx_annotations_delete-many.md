## gcx annotations delete-many

Delete multiple annotations by annotation ID, or by dashboard + panel.

### Synopsis

Delete multiple annotations in a single request.

Provide either --annotation-id, or a dashboard selector (--dashboard-uid or
--dashboard-id) together with --panel-id.

```
gcx annotations delete-many [flags]
```

### Examples

```
  gcx annotations delete-many --annotation-id 42
  gcx annotations delete-many --dashboard-uid abcdef123 --panel-id 3
```

### Options

```
      --annotation-id int      Delete a single annotation by ID
      --dashboard-id int       Dashboard numeric ID (with --panel-id)
      --dashboard-uid string   Dashboard UID (with --panel-id)
      --force                  Skip confirmation prompt
  -h, --help                   help for delete-many
      --jq string              jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string            Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string          Output format. One of: agents, json, text, yaml (default "text")
      --panel-id int           Panel ID (with a dashboard selector)
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

* [gcx annotations](gcx_annotations.md)	 - Manage Grafana annotations

