## gcx permissions set

Replace the full permission set for a resource instance.

### Synopsis

Replace the full permission set for a resource instance from a JSON file.

The file is a JSON array of assignments (or a {"permissions": [...]} object),
each with a "permission" level (View/Edit/Admin) and exactly one of
"userId", "teamId", or "builtInRole".

<resource> is one of: folders, dashboards, datasources, teams, serviceaccounts

```
gcx permissions set <resource> <id> -f FILE [flags]
```

### Examples

```
  gcx permissions set dashboards my-uid -f acl.json
  cat acl.json | gcx permissions set folders my-uid -f -
```

### Options

```
  -f, --file string   JSON file with the permission set (use - for stdin)
      --force         Skip confirmation prompt
  -h, --help          help for set
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

