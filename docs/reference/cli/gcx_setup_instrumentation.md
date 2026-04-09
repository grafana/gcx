## gcx setup instrumentation

Manage observability instrumentation for Kubernetes clusters.

### Options

```
  -h, --help   help for instrumentation
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

* [gcx setup](gcx_setup.md)	 - Onboard and configure Grafana Cloud products.
* [gcx setup instrumentation apply](gcx_setup_instrumentation_apply.md)	 - Apply an InstrumentationConfig manifest.
* [gcx setup instrumentation discover](gcx_setup_instrumentation_discover.md)	 - Discover instrumentable workloads in a cluster.
* [gcx setup instrumentation show](gcx_setup_instrumentation_show.md)	 - Show current instrumentation config as a portable manifest.
* [gcx setup instrumentation status](gcx_setup_instrumentation_status.md)	 - Show instrumentation status across clusters.

