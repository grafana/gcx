## gcx dashboards

Render Grafana dashboard snapshots

### Synopsis

Render Grafana dashboards and panels as PNG images via the Image Renderer. For dashboard CRUD operations, use 'gcx resources' with a dashboards selector.

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for dashboards
```

### Options inherited from parent commands

```
      --agent           Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --no-color        Disable color output
      --no-truncate     Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count   Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx dashboards snapshot](gcx_dashboards_snapshot.md)	 - Render dashboard snapshots as PNG images

