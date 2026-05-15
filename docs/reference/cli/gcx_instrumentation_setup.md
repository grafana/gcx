## gcx instrumentation setup

Onboard a Kubernetes cluster for Grafana Instrumentation Hub

### Synopsis

Onboard a Kubernetes cluster end-to-end by configuring K8s monitoring
and printing a runnable helm install command.

Steps performed:
  1. Calls SetupK8sDiscovery — idempotent, safe to re-run.
  2. Reads the cluster's current K8s monitoring configuration.
  3. Resolves desired flag values: prompts interactively (when stdin is a TTY
     and --use-defaults is absent) or applies defaults under --use-defaults:
       costMetrics=true  clusterEvents=true  energyMetrics=false  nodeLogs=false
     Per-flag overrides (--cost-metrics[=true|false], --cluster-events[=true|false],
     --energy-metrics[=true|false], --node-logs[=true|false]) take precedence over defaults.
  4. Calls SetK8SInstrumentation only when at least one flag changed.
  5. Emits a mutation summary to stderr.
  6. Prints a parameterized helm command to stdout.

Re-running with unchanged inputs is safe and idempotent.

The helm command installs grafana-cloud-onboarding and connects the cluster to
Grafana Cloud via Fleet Management. Replace <YOUR_CLOUD_ACCESS_TOKEN> with a
Cloud Access Policy token scoped to metrics:read and set:alloy-data-write.

```
gcx instrumentation setup <cluster> [flags]
```

### Options

```
      --cluster-events    Set clusterEvents under --use-defaults. Pass --cluster-events=false to disable. Omit to use default (true).
      --cost-metrics      Set costMetrics under --use-defaults. Pass --cost-metrics=false to disable. Omit to use default (true).
      --energy-metrics    Set energyMetrics under --use-defaults. Pass --energy-metrics=false to disable. Omit to use default (false).
  -h, --help              help for setup
      --node-logs         Set nodeLogs under --use-defaults. Pass --node-logs=false to disable. Omit to use default (false).
      --print-helm-only   Print the helm command and exit; no server calls are made
      --use-defaults      Apply defaults without prompting (required when stdin is not a TTY)
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

* [gcx instrumentation](gcx_instrumentation.md)	 - Manage Grafana Instrumentation Hub

