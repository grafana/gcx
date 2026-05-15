## gcx vulnobs

Inspect Grafana Vulnerability Observability data (read-only).

### Synopsis

Inspect Grafana Vulnerability Observability data (read-only).

The vulnerability-obs API is read-only from clients; mutations happen on the
server as scanners ingest findings. This command tree exposes groups,
projects (sources), and CVE findings (issues) through the same plugin-proxy
GraphQL endpoint the Grafana UI uses.

Source data is also available through the unified resources tier:

    gcx resources list sources.vulnobs.grafana.app


### Options

```
      --config string   Path to the configuration file to use
  -h, --help            help for vulnobs
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
* [gcx vulnobs groups](gcx_vulnobs_groups.md)	 - Manage vulnerability-obs groups (read-only).
* [gcx vulnobs projects](gcx_vulnobs_projects.md)	 - Manage vulnerability-obs projects (sources) and their findings.

