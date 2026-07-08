## gcx metrics cardinality label-names

Show the number of distinct values per label name

### Synopsis

Show, for each label name, how many distinct values it has, via the Mimir cardinality analysis API.

```
gcx metrics cardinality label-names [flags]
```

### Examples

```

  # Top label names by distinct value count (configured default datasource)
  gcx metrics cardinality label-names

  # Scope to a metric family and use active-series counting
  gcx metrics cardinality label-names -d UID --selector '{__name__=~"grafanacloud_.*"}' --count-method active

  # Output as JSON
  gcx metrics cardinality label-names -d UID -o json
```

### Options

```
      --count-method string   Series counting method: "inmemory" or "active" (default "inmemory")
  -d, --datasource string     Datasource UID (required unless datasources.prometheus is configured)
  -h, --help                  help for label-names
      --jq string             jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string           Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int             Maximum number of items to return (0-500) (default 20)
  -o, --output string         Output format. One of: agents, json, table, yaml (default "table")
      --selector string       PromQL series selector scoping the analysis
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

* [gcx metrics cardinality](gcx_metrics_cardinality.md)	 - Analyze series cardinality

