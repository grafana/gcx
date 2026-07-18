## gcx permissions grant

Grant a permission level to a single user, team, or built-in role.

### Synopsis

Grant a permission level to a single principal on a resource instance.

Specify exactly one of --user, --team, or --role, plus --level.

<resource> is one of: folders, dashboards, datasources, teams, serviceaccounts

```
gcx permissions grant <resource> <id> [flags]
```

### Examples

```
  gcx permissions grant dashboards my-uid --team 3 --level Edit
  gcx permissions grant folders my-uid --role Viewer --level View
  gcx permissions grant datasources my-uid --user alice --level Admin
```

### Options

```
  -h, --help           help for grant
      --level string   Permission level (e.g. View, Edit, Admin)
      --role string    Built-in role to grant to (e.g. Viewer, Editor, Admin)
      --team string    Team to grant to (numeric ID or UID)
      --user string    User to grant to (numeric ID or UID)
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx permissions](gcx_permissions.md)	 - Manage Grafana resource permissions (RBAC)

