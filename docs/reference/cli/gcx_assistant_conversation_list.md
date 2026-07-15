## gcx assistant conversation list

List Grafana Assistant conversations

### Synopsis

List the conversations you can access, most recently updated first.

Use this to discover conversation IDs, then pull a transcript with
'gcx assistant conversation get' or continue one with
'gcx assistant prompt --context-id'.

```
gcx assistant conversation list [flags]
```

### Examples

```
  gcx assistant conversation list
  gcx assistant conversation list --source assistant --limit 25
  gcx assistant conversation list -o json
```

### Options

```
      --archived-only      Show only archived conversations
  -h, --help               help for list
      --include-archived   Include archived conversations
      --jq string          jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string        Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int          Maximum number of conversations to return (default 15)
      --offset int         Number of conversations to skip (for pagination)
  -o, --output string      Output format. One of: agents, json, table, wide, yaml (default "table")
      --source string      Filter by conversation source. Use "all" for every source, or a comma-separated list (e.g. "assistant,cli"). (default "assistant,slack,cli")
      --timeout int        HTTP timeout in seconds (default 60)
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

* [gcx assistant conversation](gcx_assistant_conversation.md)	 - Read Grafana Assistant conversations

