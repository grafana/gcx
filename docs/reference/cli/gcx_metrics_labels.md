## gcx metrics labels

List labels or label values

### Synopsis

List all labels or get values for a specific label from a Prometheus datasource.

```
gcx metrics labels [flags]
```

### Examples

```

  # List all labels (use datasource UID, not name)
  gcx metrics labels -d UID

  # Get values for a specific label
  gcx metrics labels -d UID --label job

  # List labels present on a metric
  gcx metrics labels -d UID --metric http_requests_total

  # Get values a label takes on a metric
  gcx metrics labels -d UID --metric http_requests_total --label job

  # Scope with an arbitrary series selector
  gcx metrics labels -d UID --match '{job="api"}'

  # Output as JSON
  gcx metrics labels -d UID -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.prometheus is configured)
  -h, --help                help for labels
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of dotted field paths to include in JSON output (e.g. spec.name), or 'list' (or '?') to discover the available paths
  -l, --label string        Get values for this label (omit to list all labels)
      --match stringArray   Series selector(s) to scope results; repeatable (repeated selectors combine as a union, per the Prometheus match[] API)
      --metric string       Only results from series of this metric (narrows every --match selector)
  -o, --output string       Output format. One of: agents, json, table, yaml (default "table")
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

