## gcx metrics adaptive recommendations show

Show metric recommendations.

```
gcx metrics adaptive recommendations show [flags]
```

### Options

```
      --action stringArray   Filter by action: add, update, remove, keep (repeatable)
  -h, --help                 help for show
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string        Output format. One of: json, table, wide, yaml (default "table")
      --reverse              Reverse the default sort order
      --segment string       Segment ID
      --sort string          Sort by: metric, savings, series-before, series-after, action (default "metric")
      --top int              Limit to top N results (0 = all)
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --limit int          Maximum number of items to return from list operations (0 for all; defaults to 50 in agent mode)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx metrics adaptive recommendations](gcx_metrics_adaptive_recommendations.md)	 - Manage metric recommendations.

