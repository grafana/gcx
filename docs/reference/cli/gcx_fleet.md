## gcx fleet

Manage Grafana Fleet Management pipelines and collectors

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for fleet
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --limit int          Maximum number of items to return from list operations (0 for all; defaults to 50 in agent mode)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx fleet collectors](gcx_fleet_collectors.md)	 - Manage Fleet Management collectors.
* [gcx fleet pipelines](gcx_fleet_pipelines.md)	 - Manage Fleet Management pipelines.
* [gcx fleet tenant](gcx_fleet_tenant.md)	 - Manage Fleet Management tenant settings.

