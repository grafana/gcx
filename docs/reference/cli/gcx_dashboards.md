## gcx dashboards

Manage Grafana dashboards

### Synopsis

Create, read, update, delete, and search Grafana dashboards via the Kubernetes-compatible Grafana API.

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for dashboards
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
* [gcx dashboards create](gcx_dashboards_create.md)	 - Create a dashboard from a manifest
* [gcx dashboards delete](gcx_dashboards_delete.md)	 - Delete a dashboard
* [gcx dashboards get](gcx_dashboards_get.md)	 - Get a dashboard by name
* [gcx dashboards list](gcx_dashboards_list.md)	 - List dashboards
* [gcx dashboards search](gcx_dashboards_search.md)	 - Search dashboards by title, tag, or folder
* [gcx dashboards snapshot](gcx_dashboards_snapshot.md)	 - Render dashboard snapshots as PNG images
* [gcx dashboards update](gcx_dashboards_update.md)	 - Update a dashboard from a manifest
* [gcx dashboards versions](gcx_dashboards_versions.md)	 - Manage dashboard version history

