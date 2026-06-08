## gcx assistant investigations chat

Show the chat thread for a v2 investigation.

### Synopsis

Stream the chat thread that backs a v2 investigation: assistant prose, tool calls (search_skills, prometheus_query_handler, loki_query_handler_investigator, tempo_query_handler, ...), and tool results. The legacy report/timeline/todos endpoints return empty stubs on v2 — this command is the substantive view.

```
gcx assistant investigations chat <id> [flags]
```

### Options

```
  -h, --help             help for chat
      --include-hidden   Include hidden system messages
      --json string      Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string    Output format. One of: agents, json, table, wide, yaml (default "table")
      --role string      Filter messages by role (user|assistant|tool)
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx assistant investigations](gcx_assistant_investigations.md)	 - Manage Grafana Assistant investigations.

