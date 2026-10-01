## gcx metrics search label-names

[experimental] Search label names

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

Search label names from a Prometheus/Mimir datasource. Requires TERM (performs a fuzzy search; multiple TERM values combine as OR), or a scope instead: --metric, --metric-regex or --match.

sort_by=score (the default) requires a search term — omitting TERM in favor
of a scope falls back to sort_by=alpha, and an explicit --sort-by=score
without TERM is rejected.

--metric-regex is used exactly as given — PromQL anchors =~ at ^...$, so
"kube" matches only a metric literally named "kube", not one containing
it. Write ".*kube.*" for a contains search.

Without --from/--to or --since, the server searches only the last hour; use
--since (for example --since 7d) to look further back.

This API is experimental and disabled by default on both self-hosted
Prometheus (requires --enable-feature=search-api) and self-hosted Mimir
(requires -querier.experimental-search-api-enabled).

See also the sibling metric-name search and label-value search commands.

```
gcx metrics search label-names [TERM...] [flags]
```

### Examples

```

  # Fuzzy search label names (configured default datasource)
  gcx metrics search label-names job

  # Show label names available on a given metric
  gcx metrics search label-names --metric http_requests_total

  # Show label names on series matching a selector
  gcx metrics search label-names --match '{job="api"}'

  # Search for label names on a given metric
  gcx metrics search label-names namespace --metric http_requests_total

  # Search for label names across a range of metrics
  gcx metrics search label-names namespace --metric-regex '.*kube.*'

  # Search label names seen in the last 7 days (default: the last hour)
  gcx metrics search label-names job --since 7d

  # Output as JSON
  gcx metrics search label-names job -o json
```

### Options

```
      --case-sensitive        Case-sensitive search term matching (case-insensitive by default)
  -d, --datasource string     Datasource UID (required unless datasources.prometheus is configured)
      --from string           Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
      --fuzz-alg string       Fuzzy match algorithm: jarowinkler or subsequence (default "jarowinkler")
      --fuzz-threshold int    Minimum fuzzy match score as a percentage, 0-100; scores are reported from 0 to 1. With jarowinkler the threshold applies only to fuzzy matches: substring matches are always kept, and 0 turns fuzzy matching off (default 70)
  -h, --help                  help for label-names
      --include-score         Include each result's relevance score (0 to 1; higher is a closer match)
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
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx metrics search](gcx_metrics_search.md)	 - [experimental] Search for metric names, label names or label values

