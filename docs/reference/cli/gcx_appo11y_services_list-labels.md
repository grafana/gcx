## gcx appo11y services list-labels

Discover the labels (and values) available to --filter and --group-by for a service.

### Synopsis

List the labels present on a service's span-metric series — the exact set
that "gcx appo11y services get/list-operations --filter/--group-by" can
operate on — with each label's distinct-value count.

This answers "what can I break this service down by?" without guessing.
Pass --label <name> to list the distinct values of a single label so you
know what to feed --filter (e.g. --filter k8s_cluster_name=<value>).

Labels come from the span-metric calls series (auto-detected metrics-mode),
which is what the RED commands filter and group on. Note:

  - "services map" groups on the Tempo service-graph metric family, whose
    labels may differ (and often omit cluster labels).
  - "services get <svc> -o json" additionally surfaces the target_info
    resource attributes under .service.labels.

```
gcx appo11y services list-labels <service> [--namespace ns] [flags]
```

### Examples

```

  # What can I filter/group checkoutservice by?
  gcx appo11y services list-labels checkoutservice

  # Which clusters does it run in? (values to feed --filter/--group-by)
  gcx appo11y services list-labels checkoutservice --label k8s_cluster_name

  # JSON for scripting
  gcx appo11y services list-labels checkoutservice -o json
```

### Options

```
  -d, --datasource string     Prometheus datasource UID (defaults to datasources.prometheus in config or auto-discovery)
      --filter stringArray    Restrict discovery to series matching a label matcher, e.g. --filter k8s_cluster_name=prod-us (repeatable)
  -h, --help                  help for list-labels
      --jq string             jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string           Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --label string          Show the distinct values of a single label instead of the label summary (e.g. --label k8s_cluster_name)
      --metrics-mode string   Span-metrics family whose labels to inspect. One of: auto (probes the stack), v3, tempo, or otel (default "auto")
  -n, --namespace string      Service namespace (only needed when the argument is the bare service name and multiple namespaces are in play)
  -o, --output string         Output format. One of: agents, json, table, wide, yaml (default "table")
      --since string          Lookback window for series discovery (e.g. 15m, 1h, 1d) — PromQL duration syntax (default "1h")
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

