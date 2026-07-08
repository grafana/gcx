## gcx metrics cardinality

Analyze series cardinality

### Synopsis

Analyze series cardinality by label names and values via the Mimir cardinality analysis API.

These endpoints are available on Grafana Mimir (OSS) and Grafana Cloud; on
self-hosted Mimir they require -querier.cardinality-analysis-enabled.

### Options

```
  -h, --help   help for cardinality
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
* [gcx metrics cardinality label-names](gcx_metrics_cardinality_label-names.md)	 - Show the number of distinct values per label name
* [gcx metrics cardinality label-values](gcx_metrics_cardinality_label-values.md)	 - Show distinct values and per-value series counts for labels

