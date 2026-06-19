## gcx appo11y services get

Inspect a single Application Observability service: metadata + RED snapshot.

### Synopsis

Show metadata and a rate/errors/duration snapshot for one service.

The argument is either the bare service name (matching the OTel service.name
resource attribute) or the canonical "<namespace>/<name>" form. When a bare
name is given, the namespace is resolved automatically from target_info;
ambiguity (the same name in multiple namespaces) errors out so the snapshot
can't accidentally target the wrong service. Pass --namespace or use the
"<namespace>/<name>" form to disambiguate.

Metadata comes from the same target_info/traces_target_info union used by
"gcx appo11y services list". RED numbers are computed from span metrics
over --since (default 5m), restricted to inbound spans (SERVER + CONSUMER).

The span-metric family is selected by --metrics-mode:
  auto   probe each family for this service and pick the one with data
         (prefers v3 > tempo > otel when a stack double-emits)
  v3     traces_span_metrics_calls_total / _duration_seconds_bucket
         (OTel Collector >= 0.109, Grafana Alloy >= 1.5.0)
  tempo  traces_spanmetrics_calls_total  / _latency_bucket
         (Tempo metrics-generator — Grafana Cloud default — and Beyla)
  otel   calls_total / duration_seconds_bucket
         (OTel Collector 0.94–0.108, Alloy 1.0–1.4.3, Grafana Agent >= 0.40)
The resolved mode is reported in the snapshot output so you can confirm
which family produced the numbers.

```
gcx appo11y services get <service> [--namespace ns] [flags]
```

### Examples

```

  # Bare name — namespace is resolved from target_info
  gcx appo11y services get checkoutservice

  # Same service, explicit namespace (skips the lookup)
  gcx appo11y services get payments/checkoutservice --since 1h

  # JSON for scripting
  gcx appo11y services get checkoutservice -o json

  # Restrict to server-side traffic only
  gcx appo11y services get checkoutservice --kind server

  # Stack double-emits both families; force the Tempo metrics-generator numbers
  gcx appo11y services get checkoutservice --metrics-mode tempo
```

### Options

```
  -d, --datasource string     Prometheus datasource UID (defaults to datasources.prometheus in config or auto-discovery)
  -h, --help                  help for get
      --jq string             jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string           Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --kind string           Span kinds to include. One of: inbound (server+consumer), server, consumer, all, or a comma-separated list of SPAN_KIND_* literals (default "inbound")
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

