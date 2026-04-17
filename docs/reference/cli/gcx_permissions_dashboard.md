## gcx permissions dashboard

Manage dashboard permissions.

### Options

```
  -h, --help   help for dashboard
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

* [gcx permissions](gcx_permissions.md)	 - Manage Grafana folder and dashboard permissions
* [gcx permissions dashboard get](gcx_permissions_dashboard_get.md)	 - Get permissions for a dashboard by UID.
* [gcx permissions dashboard update](gcx_permissions_dashboard_update.md)	 - Update permissions for a dashboard from a JSON file.

