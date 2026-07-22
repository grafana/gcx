## gcx k6 projects

Manage k6 Cloud projects.

### Options

```
  -h, --help   help for projects
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

* [gcx k6](gcx_k6.md)	 - Manage Grafana k6 Cloud projects, load tests, and schedules
* [gcx k6 projects create](gcx_k6_projects_create.md)	 - Create a new k6 project from a file.
* [gcx k6 projects delete](gcx_k6_projects_delete.md)	 - Delete a k6 project.
* [gcx k6 projects get](gcx_k6_projects_get.md)	 - Get a single k6 project by ID or name.
* [gcx k6 projects list](gcx_k6_projects_list.md)	 - List k6 Cloud projects.
* [gcx k6 projects list-allowed-load-zones](gcx_k6_projects_list-allowed-load-zones.md)	 - List load zones allowed for a project.
* [gcx k6 projects update](gcx_k6_projects_update.md)	 - Update a k6 project.
* [gcx k6 projects update-allowed-load-zones](gcx_k6_projects_update-allowed-load-zones.md)	 - Replace the set of load zones allowed for a project.

