## gcx metrics search metric-names

Search metric names (experimental)

### Synopsis

Search metric names from a Prometheus/Mimir datasource. At least one TERM is required.

This API allows for metric names to be discovered via a configurable fuzzy search. 

Search terms can be augmented with matchers for additional filtering of considered series.

This API is experimental and disabled by default on both self-hosted
Prometheus (requires --enable-feature=search-api) and self-hosted Mimir
(requires -querier.experimental-search-api-enabled).

See also search label-names and search label-values.

```
gcx metrics search metric-names TERM... [flags]
```

### Examples

```

  # Fuzzy search metric names (configured default datasource)
  gcx metrics search metric-names http

  # Refine fuzzy search algorithm
  gcx metrics search metric-names http --fuzz-alg=subsequence --fuzz-threshold=70

  # Limit result sets and control ordering
  gcx metrics search metric-names http --limit=10 --sort-by=alpha 

  # Include relevance score and metric metadata
  gcx metrics search metric-names http --include-score --include-metadata

  # Output as JSON
  gcx metrics search metric-names http -o json
```

### Options

```
      --batch-size int       Maximum results per streamed batch (max 10000) (default 100)
      --case-sensitive       Case-sensitive search term matching (case-insensitive by default)
  -d, --datasource string    Datasource UID (required unless datasources.prometheus is configured)
      --from string          Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
      --fuzz-alg string      Fuzzy match algorithm: jarowinkler or subsequence (default "jarowinkler")
      --fuzz-threshold int   Minimum match score 0-100 (0: no minimum) (default 70)
  -h, --help                 help for metric-names
      --include-metadata     Include each result's metric type, help text, and unit (when available)
      --include-score        Include each result's relevance score
      --jq string            jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int            Maximum results to return (0: unlimited but may be limited server side) (default 50)
      --match stringArray    PromQL series selector(s) restricting candidates; repeatable
  -o, --output string        Output format. One of: agents, json, table, yaml (default "table")
      --since string         Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --sort-by string       Sort by: score (requires a search term) or alpha (default "score")
      --sort-dir string      Sort direction for --sort-by alpha: asc (default) or dsc
      --to string            End time (RFC3339, Unix timestamp, or relative like 'now')
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx metrics search](gcx_metrics_search.md)	 - Search for metric names, label names or label values (experimental)

