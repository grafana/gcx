## gcx permissions

Manage Grafana resource permissions (RBAC)

### Synopsis

Manage Grafana resource permissions via the granular access-control (RBAC) API.

Supported resources: folders, dashboards, datasources, teams, serviceaccounts.
Reads work on all editions; writes require RBAC (Grafana Enterprise or Cloud).

### Options

```
      --config string   Path to the configuration file to use
  -h, --help            help for permissions
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
* [gcx permissions get](gcx_permissions_get.md)	 - Get the permission list for a resource instance.
* [gcx permissions grant](gcx_permissions_grant.md)	 - Grant a permission level to a single user, team, or built-in role.
* [gcx permissions levels](gcx_permissions_levels.md)	 - Show the assignable permission levels for a resource kind.
* [gcx permissions set](gcx_permissions_set.md)	 - Replace the full permission set for a resource instance.

