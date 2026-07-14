## gcx instrumentation

Manage Grafana Instrumentation Hub

### Synopsis

Manage Grafana Instrumentation Hub using action-verb commands.

The instrumentation command tree provides:

  setup      Guided onboarding wizard: configures a cluster end-to-end and
             prints a runnable helm install command.

  status     Cross-cutting observed state for clusters and namespaces
             (RunK8sMonitoring + ListPipelines merge).

  check      Validate OpenTelemetry instrumentation for an application
             running locally (env vars, SDK, collector, Beyla, Alloy,
             Grafana Cloud connectivity).

  clusters   Declared and observed state per K8s cluster:
             list, get, configure, remove, wait.
             Sub-group "apps" manages namespace-level Beyla configuration.

  services   Workload-level observed state and per-workload inclusion
             overrides across the fleet: list, get, include, exclude, clear.

Authentication and roles: these commands reach Fleet Management through the
grafana-collector-app plugin proxy using your active Grafana credential (OAuth
included) — no Cloud access-policy token is required. Reads (list/get/status)
need the Viewer role; mutations (setup, configure, remove, include, exclude,
clear, and the K8s discovery/monitoring calls) require the Grafana Admin role.

### Options

```
      --config string   Path to the configuration file to use
  -h, --help            help for instrumentation
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx instrumentation check](gcx_instrumentation_check.md)	 - Validate OpenTelemetry instrumentation for an application
* [gcx instrumentation clusters](gcx_instrumentation_clusters.md)	 - Manage K8s monitoring configuration for clusters
* [gcx instrumentation services](gcx_instrumentation_services.md)	 - Manage workload-level instrumentation across the fleet
* [gcx instrumentation setup](gcx_instrumentation_setup.md)	 - Onboard a Kubernetes cluster for Grafana Instrumentation Hub
* [gcx instrumentation status](gcx_instrumentation_status.md)	 - Show observed instrumentation state for clusters and namespaces.

