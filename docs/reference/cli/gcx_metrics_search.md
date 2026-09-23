## gcx metrics search

Search for metric names, label names or label values (experimental)

### Synopsis

Search for metric names, label names or label values via the experimental search API.

This API is experimental and disabled by default on both self-hosted
Prometheus (requires --enable-feature=search-api) and self-hosted Mimir
(requires -querier.experimental-search-api-enabled).

### Options

```
  -h, --help   help for search
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

* [gcx metrics](gcx_metrics.md)	 - Query Prometheus datasources and manage Adaptive Metrics
* [gcx metrics search label-names](gcx_metrics_search_label-names.md)	 - Search label names (experimental)
* [gcx metrics search label-values](gcx_metrics_search_label-values.md)	 - Search the values of a label (experimental)
* [gcx metrics search metric-names](gcx_metrics_search_metric-names.md)	 - Search metric names (experimental)

