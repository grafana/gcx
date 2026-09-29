## gcx datasources prometheus search-label-values

Search the values of a label (experimental)

### Synopsis

Search the values of a single label from a Prometheus/Mimir datasource. LABEL is always required; TERM (multiple values combine as OR), --metric, and --metric-regex are all optional and may be combined or omitted — LABEL alone lists every value of that label.

sort_by=score (the default) requires a search term — omitting TERM falls
back to sort_by=alpha unless --sort-by is set explicitly.

--metric-regex is used exactly as given — PromQL anchors =~ at ^...$, so
"kube" matches only a metric literally named "kube", not one containing
it. Write ".*kube.*" for a contains search.

This API is experimental and disabled by default on both self-hosted
Prometheus (requires --enable-feature=search-api) and self-hosted Mimir
(requires -querier.experimental-search-api-enabled).

See also the sibling metric-name search and label-name search commands.

```
gcx datasources prometheus search-label-values LABEL [TERM...] [flags]
```

### Examples

```

  # List every value of the "job" label (use datasource UID, not name)
  gcx datasources prometheus search-label-values job -d UID

  # Fuzzy search values of the "job" label
  gcx datasources prometheus search-label-values job pro -d UID

  # List every "job" label value present on a specific metric
  gcx datasources prometheus search-label-values job -d UID --metric http_requests_total

  # List every "job" label value present on a range of metrics
  gcx datasources prometheus search-label-values job -d UID --metric-regex '.*kube.*'

  # Output as JSON
  gcx datasources prometheus search-label-values job pro -d UID -o json
```

### Options

```
      --case-sensitive        Case-sensitive search term matching (case-insensitive by default)
  -d, --datasource string     Datasource UID (required unless datasources.prometheus is configured)
      --from string           Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
      --fuzz-alg string       Fuzzy match algorithm: jarowinkler or subsequence (default "jarowinkler")
      --fuzz-threshold int    Minimum fuzzy match score 0-100 (with jarowinkler, 0 disables fuzzy matching, leaving substring matches only) (default 70)
  -h, --help                  help for search-label-values
      --include-score         Include each result's relevance score
      --jq string             jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string           Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int             Maximum results to return (0: unlimited on Mimir, subject to server-side caps; Prometheus requires a positive value) (default 50)
      --match stringArray     PromQL series selector(s) restricting candidates; repeatable (repeated selectors combine as a union, per the Prometheus match[] API)
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

