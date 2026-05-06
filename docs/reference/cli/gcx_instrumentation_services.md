## gcx instrumentation services

Manage workload-level instrumentation across the fleet

### Synopsis

Manage workload-level observed state and per-workload inclusion
overrides across the fleet.

Subcommands:

  list     List all discovered workloads, with optional filtering.
  get      Get a single workload by (cluster, namespace, service).
  include  Include a workload for instrumentation (DWIM, idempotent).
  exclude  Exclude a workload from instrumentation (DWIM, idempotent).
  clear    Remove a per-workload override, inheriting namespace default.

### Options

```
  -h, --help   help for services
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
* [gcx instrumentation services clear](gcx_instrumentation_services_clear.md)	 - Remove a per-workload override, inheriting namespace default
* [gcx instrumentation services exclude](gcx_instrumentation_services_exclude.md)	 - Exclude a workload from instrumentation (DWIM, idempotent)
* [gcx instrumentation services get](gcx_instrumentation_services_get.md)	 - Get a single discovered workload by (cluster, namespace, service)
* [gcx instrumentation services include](gcx_instrumentation_services_include.md)	 - Include a workload for instrumentation (DWIM, idempotent)
* [gcx instrumentation services list](gcx_instrumentation_services_list.md)	 - List discovered workloads across the fleet

