## gcx instrumentation clusters apps

Manage namespace-level Beyla instrumentation for a cluster

### Synopsis

Manage namespace-level Beyla auto-instrumentation configuration for a cluster.

Commands operate on the declared state stored in Grafana Cloud's
instrumentation service (GetAppInstrumentation / SetAppInstrumentation).
The wait command reads observed state from RunK8sDiscovery.

Identity is positional: configure and remove take <cluster> and <namespace> as
the first two positional arguments.

### Options

```
  -h, --help   help for apps
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

* [gcx instrumentation clusters](gcx_instrumentation_clusters.md)	 - Manage K8s monitoring configuration for clusters
* [gcx instrumentation clusters apps configure](gcx_instrumentation_clusters_apps_configure.md)	 - Configure Beyla instrumentation for a namespace
* [gcx instrumentation clusters apps get](gcx_instrumentation_clusters_apps_get.md)	 - Get the app instrumentation entry for a single namespace
* [gcx instrumentation clusters apps list](gcx_instrumentation_clusters_apps_list.md)	 - List all namespace app instrumentation entries for a cluster
* [gcx instrumentation clusters apps remove](gcx_instrumentation_clusters_apps_remove.md)	 - Remove Beyla instrumentation for a namespace
* [gcx instrumentation clusters apps wait](gcx_instrumentation_clusters_apps_wait.md)	 - Wait for namespace instrumentation to reach a stable state

