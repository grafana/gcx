## gcx sigil conversations search

Search conversations with filters.

### Synopsis

Search conversations using filter expressions and time ranges.

Defaults to the last 24 hours. Use --from and --to for custom ranges (both required).

Filter syntax: key operator "value" (multiple filters separated by spaces).

Filter keys (trace): model, provider, agent, agent.version, status,
  error.type, error.category, duration, tool.name, operation, namespace, cluster, service
Filter keys (metadata): generation_count, eval.passed, eval.evaluator_id, eval.score_key, eval.score
Operators: =, !=, >, <, >=, <=, =~ (regex)

Returns a single page of results (controlled by --page-size). A warning is
shown when more results are available.

```
gcx sigil conversations search [flags]
```

### Examples

```
  gcx sigil conversations search --filters 'agent = "claude-code"'
  gcx sigil conversations search --filters 'agent = "claude-code" model = "claude-opus-4-6"'
  gcx sigil conversations search --filters 'status = "error"' --from 2026-04-01T00:00:00Z --to 2026-04-02T00:00:00Z
```

### Options

```
      --filters string   Filter expression for conversation search
      --from string      Start of time range (RFC3339, e.g. 2026-01-01T00:00:00Z)
  -h, --help             help for search
      --json string      Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string    Output format. One of: json, table, wide, yaml (default "table")
      --page-size int    Number of results per page (default 50)
      --to string        End of time range (RFC3339, e.g. 2026-12-31T23:59:59Z)
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx sigil conversations](gcx_sigil_conversations.md)	 - Query Sigil conversations.

