## gcx metrics list

List metric names

### Synopsis

List metric names from a Prometheus datasource via the label values endpoint for `__name__`.
Scope the server-side lookup with --match selectors; filter names client-side
with --prefix, --suffix, and --contains, which combine with AND.
Output is capped at 100 names by default; pass --limit 0 for the full list.

```
gcx metrics list [flags]
```

### Examples

```

  # List metric names (first 100 by default; use datasource UID, not name)
  gcx metrics list -d UID

  # Find cart-related metrics
  gcx metrics list -d UID --contains cart

  # Counters only
  gcx metrics list -d UID --suffix _total

  # Metrics present on a job
  gcx metrics list -d UID --match '{job="api"}'

  # Output as JSON
  gcx metrics list -d UID -o json
```

### Options

```
      --contains string     Only include names containing this string
  -d, --datasource string   Datasource UID (required unless datasources.prometheus is configured)
  -h, --help                help for list
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int           Maximum number of names to return after filtering (0 for all) (default 100)
      --match stringArray   Series selector(s) to scope results; repeatable (repeated selectors combine as a union, per the Prometheus match[] API)
  -o, --output string       Output format. One of: agents, json, table, yaml (default "table")
      --prefix string       Only include names starting with this string
      --suffix string       Only include names ending with this string
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

* [gcx metrics](gcx_metrics.md)	 - Query Prometheus datasources and manage Adaptive Metrics

