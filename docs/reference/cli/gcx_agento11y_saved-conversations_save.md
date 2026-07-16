## gcx agento11y saved-conversations save

Bookmark an existing live conversation as a saved conversation.

### Synopsis

Bookmark a live conversation surfaced by gcx agento11y conversations.
By default the bookmark ID is derived as saved-<conversation-id>, matching the
plugin UI; pass --saved-id to override.

```
gcx agento11y saved-conversations save <conversation-id> [flags]
```

### Examples

```
  # Bookmark with the default saved ID.
  gcx agento11y saved-conversations save conv-123 --name "Regression seed"

  # Bookmark with tags.
  gcx agento11y saved-conversations save conv-123 --name "Regression seed" --tag suite=checkout --tag priority=high
```

### Options

```
  -h, --help              help for save
      --jq string         jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string       Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --name string       Human-readable name for the bookmark (required)
  -o, --output string     Output format. One of: agents, json, yaml (default "json")
      --saved-id string   Bookmark ID; defaults to saved-<conversation-id>
      --tag stringArray   Tag in key=value form (repeatable)
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

* [gcx agento11y saved-conversations](gcx_agento11y_saved-conversations.md)	 - Bookmark live conversations as fixed inputs for evaluation runs.

