## gcx fleet

Manage Grafana Fleet Management pipelines and collectors

### Synopsis

Manage Grafana Fleet Management pipelines and collectors.

Authentication and roles: fleet commands reach Fleet Management through the
grafana-collector-app plugin proxy using your active Grafana credential (OAuth
included) — no Cloud access-policy token is required. Reads (list, get) need the
Viewer role; mutations (create, update, delete) require the Grafana Admin role.

### Options

```
      --config string   Path to the configuration file to use
  -h, --help            help for fleet
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
* [gcx fleet collectors](gcx_fleet_collectors.md)	 - Manage Fleet Management collectors.
* [gcx fleet pipelines](gcx_fleet_pipelines.md)	 - Manage Fleet Management pipelines.
* [gcx fleet tenant](gcx_fleet_tenant.md)	 - Manage Fleet Management tenant settings.

