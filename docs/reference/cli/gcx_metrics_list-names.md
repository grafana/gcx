## gcx metrics list-names

List metric names

### Synopsis

List metric names from a Prometheus datasource via the label values endpoint for `__name__`.
Scope the server-side lookup with --match selectors; filter names client-side
with --prefix, --suffix, and --contains, which combine with AND.
Output is capped at 100 names by default; pass --limit 0 for the full list.

```
gcx metrics list-names [flags]
```

### Examples

```

  # List metric names (first 100 by default; use datasource UID, not name)
  gcx metrics list-names -d UID

  # Find cart-related metrics
  gcx metrics list-names -d UID --contains cart

  # Counters only
  gcx metrics list-names -d UID --suffix _total

  # Metrics present on a job
  gcx metrics list-names -d UID --match '{job="api"}'

  # Output as JSON
  gcx metrics list-names -d UID -o json
```

### Options

```
      --contains string     Only include names containing this string (case-sensitive)
  -d, --datasource string   Datasource UID (required unless datasources.prometheus is configured)
  -h, --help                help for list-names
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of dotted field paths to include in JSON output (e.g. spec.name), or 'list' (or '?') to discover the available paths
      --limit int           Maximum number of metric names to return. 0 means all results are returned (default 100)
      --match stringArray   Series selector(s) to scope results; repeatable (repeated selectors combine as a union, per the Prometheus match[] API)
  -o, --output string       Output format. One of: agents, json, table, yaml (default "table")
      --prefix string       Only include names starting with this string (case-sensitive)
      --suffix string       Only include names ending with this string (case-sensitive)
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

