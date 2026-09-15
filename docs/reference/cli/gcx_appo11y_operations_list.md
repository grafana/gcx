## gcx appo11y operations list

Rank operations (span names) fleet-wide by time share, across every App Observability service.

### Synopsis

Rank the top operations across every service by busy-seconds-per-second
(time share), the fleet-wide counterpart to "gcx appo11y services
list-operations" (which is scoped to one service).

Each row is qualified by the service (job) it belongs to. Time-share is
normalized against the WHOLE FLEET's busy-time, not per-service — so a
--limit N view never claims the top N operations are 100% of anything;
it reports what share of the fleet's total wall-clock time they actually
consume.

The source span-metrics series (Tempo's traces_spanmetrics_*, the v3
traces_span_metrics_*, or bare OTel calls_total) is auto-detected by
default. Use --metrics-mode to pin it.

```
gcx appo11y operations list [flags]
```

### Examples

```

  # Top 20 operations fleet-wide in the default 5m window
  gcx appo11y operations list

  # Top 50, last hour, JSON for scripting
  gcx appo11y operations list --since 1h --limit 50 -o json

  # Restrict to one namespace after ranking
  gcx appo11y operations list --namespace payments

  # Break each operation out per cluster to spot per-cluster hotspots
  gcx appo11y operations list --group-by k8s_cluster_name
```

### Options

```
  -d, --datasource string     Prometheus datasource UID (defaults to datasources.prometheus in config or auto-discovery)
      --env string            Restrict to a single deployment_environment (post-query convenience filter, applied after ranking; only useful if the span metrics carry the label)
      --filter stringArray    Scope the ranking to series matching a label matcher, e.g. --filter k8s_cluster_name=prod-us (repeatable); the label must exist on the span metrics
      --group-by strings      Break each operation out per distinct value of a label, e.g. --group-by k8s_cluster_name (comma-separated or repeatable); the label must exist on the span metrics
  -h, --help                  help for list
      --jq string             jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string           Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --kg string             Knowledge Graph catalog consumption: auto (annotate rows with what the graph knows, when it's active) or off (never contact the Knowledge Graph) (default "auto")
      --kind string           Span kinds to include. One of: inbound (server+consumer), server, consumer, all, or a comma-separated list of SPAN_KIND_* literals (default "inbound")
      --limit int             Rank the top N operations fleet-wide by busy-seconds-per-second (must be 1-500; unlike other list commands, 0 is rejected — the unbounded fleet shape is #services x #operations) (default 20)
      --metrics-mode string   Span-metrics family. One of: auto (probes the stack), v3 (traces_span_metrics_*), tempo (traces_spanmetrics_*), or otel (bare calls_total + duration_seconds_bucket) (default "auto")
  -n, --namespace string      Restrict to services in a single namespace (post-query convenience filter, applied after ranking)
  -o, --output string         Output format. One of: agents, json, table, wide, yaml (default "table")
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

