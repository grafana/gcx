## gcx setup

Onboard and configure Grafana Cloud products.

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for setup
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx setup instrumentation](gcx_setup_instrumentation.md)	 - Manage observability instrumentation for Kubernetes clusters.
* [gcx setup run](gcx_setup_run.md)	 - Interactive orchestrator for product setup.
* [gcx setup status](gcx_setup_status.md)	 - Show aggregated setup status across all products.

