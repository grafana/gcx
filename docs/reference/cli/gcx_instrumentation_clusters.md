## gcx instrumentation clusters

Manage K8s monitoring configuration for clusters

### Synopsis

Manage K8s monitoring configuration for clusters.

Subcommands:
  list      List all clusters with their instrumentation status
  get       Show declared config and observed status for a single cluster
  configure Configure K8s monitoring flags (RMW or apply defaults)
  remove    Remove K8s monitoring from a cluster
  wait      Wait until a cluster reaches INSTRUMENTED status
  apps      Manage namespace-level Beyla instrumentation for a cluster

### Options

```
  -h, --help   help for clusters
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

* [gcx instrumentation](gcx_instrumentation.md)	 - Manage Grafana Instrumentation Hub
* [gcx instrumentation clusters apps](gcx_instrumentation_clusters_apps.md)	 - Manage namespace-level Beyla instrumentation for a cluster
* [gcx instrumentation clusters configure](gcx_instrumentation_clusters_configure.md)	 - Configure K8s monitoring flags on a cluster
* [gcx instrumentation clusters get](gcx_instrumentation_clusters_get.md)	 - Show declared config and observed status for a cluster
* [gcx instrumentation clusters list](gcx_instrumentation_clusters_list.md)	 - List all clusters with their instrumentation status
* [gcx instrumentation clusters remove](gcx_instrumentation_clusters_remove.md)	 - Remove K8s monitoring from a cluster
* [gcx instrumentation clusters wait](gcx_instrumentation_clusters_wait.md)	 - Wait until a cluster reaches INSTRUMENTED status

