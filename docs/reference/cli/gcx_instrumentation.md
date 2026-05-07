## gcx instrumentation

Manage Grafana Instrumentation Hub

### Synopsis

Manage Grafana Instrumentation Hub using action-verb commands.

The instrumentation command tree provides:

  clusters   Declared and observed state per K8s cluster:
             list, get, configure, remove, wait.
             Sub-group "apps" manages namespace-level Beyla configuration.

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

