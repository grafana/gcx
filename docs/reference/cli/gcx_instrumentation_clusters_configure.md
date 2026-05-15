## gcx instrumentation clusters configure

Configure K8s monitoring flags on a cluster

### Synopsis

Configure K8s monitoring feature flags on a cluster.

Two mutually exclusive modes:

  --use-defaults --yes
      Apply canonical defaults, overwriting current state. Requires --yes.
      Defaults: costMetrics=true, clusterEvents=true, energyMetrics=false, nodeLogs=false.

  --<feat>[=true|false] (one or more)
      Set listed features; unspecified features preserve their current value (RMW).
      Idempotent. No confirmation required.

Combining --use-defaults with any --<feat> flag is an error.

```
gcx instrumentation clusters configure <cluster> [flags]
```

### Examples

```
  # Apply defaults to cluster "prod-eu"
  gcx instrumentation clusters configure prod-eu --use-defaults --yes

  # Enable cost metrics, preserving other flags (RMW)
  gcx instrumentation clusters configure prod-eu --cost-metrics

  # Disable node logs (RMW)
  gcx instrumentation clusters configure prod-eu --node-logs=false
```

### Options

```
      --cluster-events   Set clusterEvents. Pass --cluster-events=false to disable.
      --cost-metrics     Set costMetrics. Pass --cost-metrics=false to disable. Omit to preserve current value.
      --energy-metrics   Set energyMetrics. Pass --energy-metrics=false to disable.
  -h, --help             help for configure
      --node-logs        Set nodeLogs. Pass --node-logs=false to disable.
      --use-defaults     Apply canonical defaults (costMetrics=true, clusterEvents=true, energyMetrics=false, nodeLogs=false). Requires --yes.
      --yes              Confirm the --use-defaults operation (required with --use-defaults)
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use (overrides current-context in config)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx instrumentation clusters](gcx_instrumentation_clusters.md)	 - Manage K8s monitoring configuration for clusters

