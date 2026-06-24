## gcx instrumentation services list

List discovered workloads across the fleet

### Synopsis

List all workloads discovered by the Beyla survey collector.

Calls RunK8sDiscovery() (fleet-wide RPC) and applies client-side filters.

Examples:
  # List all workloads
  gcx instrumentation services list

  # Filter to a specific cluster
  gcx instrumentation services list --cluster prod-1

  # Filter to a specific namespace
  gcx instrumentation services list --namespace checkout

  # Show only workloads in terminal error state
  gcx instrumentation services list --status=ERROR

  # Wide output with extra columns
  gcx instrumentation services list -o wide

Workload-level Selection / override state is not surfaced on this command. To inspect a workload's override, run "gcx instrumentation clusters apps list --cluster=<cluster> --namespace=<namespace>".

```
gcx instrumentation services list [flags]
```

### Options

```
  -A, --all                Show services from all namespaces (fleet-wide)
      --cluster string     Filter by cluster name
  -h, --help               help for list
      --jq string          jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string        Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -n, --namespace string   Filter by Kubernetes namespace
  -o, --output string      Output format. One of: agents, json, text, wide, yaml (default "text")
      --status string      Filter by instrumentation status (e.g. ERROR, INSTRUMENTED)
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

* [gcx instrumentation services](gcx_instrumentation_services.md)	 - Manage workload-level instrumentation across the fleet

