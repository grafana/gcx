## gcx appo11y services list-operations

List a service's operations with per-operation RED (span_name × rate/errors/latency).

### Synopsis

List the per-operation RED breakdown for one service.

The argument is either the bare service name or the canonical
"<namespace>/<name>" form; bare names are auto-resolved against
target_info the same way as "gcx appo11y services get".

Rows are sorted by time-share desc — the share of the service's total
wall-clock time each operation consumes, computed as
(avg_latency × rate) / sum(all). This surfaces operations that dominate
latency regardless of whether they're high-rate-fast or low-rate-slow.

The source span-metrics series (Tempo's traces_spanmetrics_*, the v3
traces_span_metrics_*, or bare OTel calls_total) is auto-detected by
default. Use --metrics-mode to pin it; see "gcx appo11y services get
--help" for the full reference.

```
gcx appo11y services list-operations <service> [--namespace ns] [flags]
```

### Examples

```

  # Top operations for the "checkoutservice" service in the default 5m window
  gcx appo11y services list-operations checkoutservice

  # Wider view with p50/p99 and absolute error rate
  gcx appo11y services list-operations checkoutservice -o wide

  # Last hour, unlimited rows, JSON for scripting
  gcx appo11y services list-operations payments/checkoutservice --since 1h --limit 0 -o json
```

### Options

```
  -d, --datasource string     Prometheus datasource UID (defaults to datasources.prometheus in config or auto-discovery)
  -h, --help                  help for list-operations
      --jq string             jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string           Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --kind string           Span kinds to include. One of: inbound (server+consumer), server, consumer, all, or a comma-separated list of SPAN_KIND_* literals (default "inbound")
      --limit int             Limit the number of operations returned (0 = unlimited; applied after sorting by time-share desc) (default 15)
      --metrics-mode string   Span-metrics family. One of: auto (probes the stack), v3 (traces_span_metrics_*), tempo (traces_spanmetrics_*), or otel (bare calls_total + duration_seconds_bucket) (default "auto")
  -n, --namespace string      Service namespace (only needed when the argument is the bare service name and multiple namespaces are in play)
  -o, --output string         Output format. One of: agents, json, table, wide, yaml (default "table")
      --since string          Rate/quantile window applied to span metrics (e.g. 1m, 5m, 1h, 1d) — PromQL duration syntax (default "5m")
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

* [gcx appo11y services](gcx_appo11y_services.md)	 - Inspect Application Observability services discovered from telemetry

