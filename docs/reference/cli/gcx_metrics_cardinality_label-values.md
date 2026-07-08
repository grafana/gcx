## gcx metrics cardinality label-values

Show distinct values and per-value series counts for labels

### Synopsis

Show, for each requested label name, its distinct values and the number of series per value, via the Mimir cardinality analysis API.

```
gcx metrics cardinality label-values [flags]
```

### Examples

```

  # Per-value series counts for one label
  gcx metrics cardinality label-values -d UID --label job

  # Multiple labels, capped and as JSON
  gcx metrics cardinality label-values -d UID --label job --label instance --limit 50 -o json
```

### Options

```
      --count-method string   Series counting method: "inmemory" or "active" (default "inmemory")
  -d, --datasource string     Datasource UID (required unless datasources.prometheus is configured)
  -h, --help                  help for label-values
      --jq string             jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string           Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -l, --label stringArray     Label name to analyze; repeatable (required)
      --limit int             Maximum number of items to return per label (0-500) (default 20)
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

