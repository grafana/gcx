## gcx instrumentation

Manage Grafana Instrumentation Hub

### Synopsis

Manage Grafana Instrumentation Hub using action-verb commands.

The instrumentation command tree provides:

  setup      Guided onboarding wizard: configures a cluster end-to-end and
             prints a runnable helm install command.

  status     Cross-cutting observed state for clusters and namespaces
             (RunK8sMonitoring + ListPipelines merge).

  clusters   Declared and observed state per K8s cluster:
             list, get, configure, remove, wait.
             Sub-group "apps" manages namespace-level Beyla configuration.

  services   Workload-level observed state and per-workload inclusion
             overrides across the fleet: list, get, include, exclude, clear.

### Options

```
      --config string   Path to the configuration file to use
  -h, --help            help for instrumentation
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --context string     Name of the context to use (overrides current-context in config)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx instrumentation clusters](gcx_instrumentation_clusters.md)	 - Manage K8s monitoring configuration for clusters
* [gcx instrumentation services](gcx_instrumentation_services.md)	 - Manage workload-level instrumentation across the fleet
* [gcx instrumentation setup](gcx_instrumentation_setup.md)	 - Onboard a Kubernetes cluster for Grafana Instrumentation Hub
* [gcx instrumentation status](gcx_instrumentation_status.md)	 - Show observed instrumentation state for clusters and namespaces.

