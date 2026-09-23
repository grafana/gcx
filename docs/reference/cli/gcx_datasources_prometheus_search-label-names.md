## gcx datasources prometheus search-label-names

Search label names (experimental)

### Synopsis

Search label names from a Prometheus/Mimir datasource. Requires TERM (performs a fuzzy search), or --metric / --metric-regex to scope by metric name instead; at least one of the three is required. sort_by=score (the default) requires a search term — omitting TERM in favor of a metric scope falls back to sort_by=alpha unless --sort-by is set explicitly.

--metric-regex is used exactly as given — PromQL anchors =~ at ^...$, so
"kube" matches only a metric literally named "kube", not one containing
it. Write ".*kube.*" for a contains search.

This API is experimental and disabled by default on both self-hosted
Prometheus (requires --enable-feature=search-api) and self-hosted Mimir
(requires -querier.experimental-search-api-enabled).

See also search-metric-names (the metric-name counterpart) and
search-label-values (search a label's values instead of label names).

```
gcx datasources prometheus search-label-names [TERM...] [flags]
```

### Examples

```

  # Fuzzy search label names (use datasource UID, not name)
  gcx datasources prometheus search-label-names job -d UID

  # Show all label names available on a given metric
  gcx datasources prometheus search-label-names -d UID --metric http_requests_total

  # Search for label names on a given metric
  gcx datasources prometheus search-label-names namespace -d UID --metric http_requests_total

  # Search for label names across a range of metrics
  gcx datasources prometheus search-label-names namespace -d UID --metric-regex '.*kube.*'

  # Output as JSON
  gcx datasources prometheus search-label-names job -d UID -o json
```

### Options

```
      --batch-size int        Maximum results per streamed batch (max 10000) (default 100)
      --case-sensitive        Case-sensitive search term matching (case-insensitive by default)
  -d, --datasource string     Datasource UID (required unless datasources.prometheus is configured)
      --from string           Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
      --fuzz-alg string       Fuzzy match algorithm: jarowinkler or subsequence (default "jarowinkler")
      --fuzz-threshold int    Minimum match score 0-100 (0: no minimum) (default 70)
  -h, --help                  help for search-label-names
      --include-score         Include each result's relevance score
      --jq string             jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string           Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int             Maximum results to return (0: unlimited but may be limited server side) (default 50)
      --match stringArray     PromQL series selector(s) restricting candidates; repeatable
      --metric string         Only results from series of this metric name; mutually exclusive with --metric-regex
      --metric-regex string   Only results from series whose metric name matches this regex. Used exactly as given. To match all metric names which contain "kube" use ".*kube.*". Mutually exclusive with --metric.
  -o, --output string         Output format. One of: agents, json, table, yaml (default "table")
      --since string          Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --sort-by string        Sort by: score (requires a search term) or alpha (default "score")
      --sort-dir string       Sort direction for --sort-by alpha: asc (default) or dsc
      --to string             End time (RFC3339, Unix timestamp, or relative like 'now')
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

* [gcx datasources prometheus](gcx_datasources_prometheus.md)	 - Query Prometheus datasources

