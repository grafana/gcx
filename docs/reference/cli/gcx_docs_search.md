## gcx docs search

Search Grafana documentation.

### Synopsis

Search the documentation index by keyword. Returns matching pages ranked by relevance.

```
gcx docs search <query> [flags]
```

### Examples

```
  # Search across all products
  gcx docs search "rate limiting"

  # Scope the search to one product
  gcx docs search "metrics generator" --product tempo

  # Return more results as JSON
  gcx docs search dashboards --limit 10 -o json
```

### Options

```
  -h, --help             help for search
      --jq string        jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string      Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int        Maximum number of results (default 5)
  -o, --output string    Output format. One of: agents, json, text, yaml (default "text")
      --product string   Filter results to a specific product
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx docs](gcx_docs.md)	 - Search and read Grafana documentation.

