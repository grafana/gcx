## gcx instrumentation clusters discover

Discover instrumentable workloads in a cluster.

### Synopsis

List workloads reported by the Alloy collector installed in <cluster>.

Results reflect Alloy collector-reported state — workloads appear only after
Alloy is installed and reporting to Grafana Cloud (refresh ~30s). An empty
result does not imply an empty cluster; it means Alloy has not yet reported
any workloads from that cluster.

For direct cluster inspection (e.g., before Alloy is installed), use
kubectl against your kubeconfig:

  kubectl get pods --all-namespaces

```
gcx instrumentation clusters discover <cluster> [flags]
```

### Options

```
  -h, --help            help for discover
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: json, table, wide, yaml (default "table")
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx instrumentation clusters](gcx_instrumentation_clusters.md)	 - Manage Kubernetes cluster monitoring configuration.

