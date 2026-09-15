## gcx appo11y operations get

Inspect a single operation (span name) within one service: RED snapshot + its share of the service's time.

### Synopsis

Show the rate/errors/duration snapshot for one operation (span_name)
inside one service.

The operation name is the positional argument; the parent service is
always given via --service (bare name or "<namespace>/<name>"), never as
part of the positional — span names routinely contain "/" (e.g.
"GET /api/v1/carts"), which would make a slash-composite positional
ambiguous.

TimeSharePercent here means this operation's share of ITS SERVICE's
wall-clock time — the same number "gcx appo11y services list-operations"
reports for the same operation — not the fleet's (see
"gcx appo11y operations list" for the fleet-wide ranking).

```
gcx appo11y operations get <operation> --service <service> [--namespace ns] [flags]
```

### Examples

```

  # One operation within the "checkoutservice" service
  gcx appo11y operations get "GET /cart" --service checkoutservice

  # Explicit namespace, last hour, JSON for scripting
  gcx appo11y operations get "GET /cart" --service payments/checkoutservice --since 1h -o json
```

### Options

```
  -d, --datasource string     Prometheus datasource UID (defaults to datasources.prometheus in config or auto-discovery)
      --filter stringArray    Scope the lookup to series matching a label matcher, e.g. --filter k8s_cluster_name=prod-us (repeatable); the label must exist on the span metrics
  -h, --help                  help for get
      --jq string             jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string           Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --kg string             Knowledge Graph catalog consumption: auto (annotate rows with what the graph knows, when it's active) or off (never contact the Knowledge Graph) (default "auto")
      --kind string           Span kinds to include. One of: inbound (server+consumer), server, consumer, all, or a comma-separated list of SPAN_KIND_* literals (default "inbound")
      --metrics-mode string   Span-metrics family. One of: auto (probes the stack), v3 (traces_span_metrics_*), tempo (traces_spanmetrics_*), or otel (bare calls_total + duration_seconds_bucket) (default "auto")
  -n, --namespace string      Service namespace (only needed when --service is a bare name and multiple namespaces are in play)
  -o, --output string         Output format. One of: agents, json, table, yaml (default "table")
      --service string        Service the operation belongs to: bare name or the canonical "<namespace>/<name>" form (required)
      --since string          Rate/quantile window applied to span metrics (e.g. 1m, 5m, 1h, 1d) — PromQL duration syntax (default "5m")
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

* [gcx appo11y operations](gcx_appo11y_operations.md)	 - Rank and inspect operations (span names) across App Observability services

