## gcx appo11y services map

Show a service's neighbourhood: who calls it (callers) and who it calls (callees).

### Synopsis

Render the service-graph slice for one service: a callers list (services
calling into this one) and a callees list (services this one calls).

Data comes from Tempo's service-graph metric family
(traces_service_graph_request_*), which is consistent across every
metrics-mode — so there's no --metrics-mode flag here.

The argument is either the bare service name or the canonical
"<namespace>/<name>" form; bare names are auto-resolved against
target_info the same way "gcx appo11y services get" does.

Latency is direction-aware: callers see the server-side p95
(how long this service took to respond), callees see the client-side
p95 (how long this service waited on the peer).

Connection type is empty for HTTP/gRPC peers; "database",
"messaging", or "virtual_node" for typed edges. Virtual-node peers
are uninstrumented callers Tempo synthesises from orphan spans.

Beyond --output table/wide/json/yaml, --output mermaid and
--output dot render the map as a Mermaid or Graphviz graph,
suitable for inlining in markdown / piping to "dot -Tpng".

```
gcx appo11y services map <service> [--namespace ns] [flags]
```

### Examples

```

  # Default two-section table
  gcx appo11y services map checkoutservice

  # Mermaid graph — paste into a markdown doc or a PR comment
  gcx appo11y services map checkoutservice -o mermaid

  # Graphviz DOT — pipe to dot for an image
  gcx appo11y services map checkoutservice -o dot | dot -Tpng -o map.png

  # Wide table with p95 / connection-type, last hour
  gcx appo11y services map payments/checkoutservice --since 1h -o wide

  # JSON for scripting
  gcx appo11y services map checkoutservice -o json

  # Break a multi-cluster service's map down to a single cluster
  gcx appo11y services map faro-collector --filter k8s_cluster_name=prod-us-central-0

  # Split each edge per cluster (service-graph metrics must carry the label)
  gcx appo11y services map faro-collector --group-by k8s_cluster_name
```

### Options

```
  -d, --datasource string    Prometheus datasource UID (defaults to datasources.prometheus in config or auto-discovery)
      --filter stringArray   Scope the map to service-graph edges matching a label matcher, e.g. --filter k8s_cluster_name=prod-us (repeatable). Use to break a multi-cluster/multi-region service down one cluster at a time; the label must exist on the service-graph metrics
      --group-by strings     Split each edge per distinct value of a label, e.g. --group-by k8s_cluster_name (comma-separated or repeatable). The label must exist on the service-graph metrics — note the Tempo service-graph family often omits cluster labels, in which case no edges match
  -h, --help                 help for map
      --jq string            jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -n, --namespace string     Service namespace (only needed when the argument is the bare service name and multiple namespaces are in play)
  -o, --output string        Output format. One of: agents, dot, json, mermaid, table, wide, yaml (default "table")
      --since string         Rate/quantile window applied to service-graph metrics (e.g. 1m, 5m, 1h, 1d) — PromQL duration syntax (default "5m")
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

