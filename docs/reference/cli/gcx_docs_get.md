## gcx docs get

Fetch a Grafana documentation page.

### Synopsis

Fetch a documentation page as cleaned markdown. Supports section extraction and offset/limit paging for bounded retrieval.

```
gcx docs get <url> [flags]
```

### Examples

```
  # Fetch the first page of a doc
  gcx docs get https://grafana.com/docs/tempo/latest/

  # Extract a single section
  gcx docs get https://grafana.com/docs/tempo/latest/ --section "Configuration"

  # Page through a long doc
  gcx docs get https://grafana.com/docs/tempo/latest/ --offset 80 --limit 80
```

### Options

```
  -h, --help             help for get
      --jq string        jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string      Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int        Maximum lines to return (0 = default)
      --offset int       Line offset for paging (0-indexed)
  -o, --output string    Output format. One of: agents, json, text, yaml (default "text")
      --section string   Heading text to extract (returns only that section)
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx docs](gcx_docs.md)	 - Search and read Grafana documentation.

