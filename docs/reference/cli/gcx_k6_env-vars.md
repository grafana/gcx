## gcx k6 env-vars

Manage k6 Cloud environment variables.

### Options

```
  -h, --help   help for env-vars
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --limit int          Maximum number of items to return from list operations (0 for all; defaults to 50 in agent mode)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx k6](gcx_k6.md)	 - Manage Grafana k6 Cloud projects, load tests, and schedules
* [gcx k6 env-vars create](gcx_k6_env-vars_create.md)	 - Create a k6 environment variable from a file.
* [gcx k6 env-vars delete](gcx_k6_env-vars_delete.md)	 - Delete a k6 environment variable.
* [gcx k6 env-vars list](gcx_k6_env-vars_list.md)	 - List k6 environment variables.
* [gcx k6 env-vars update](gcx_k6_env-vars_update.md)	 - Update a k6 environment variable.

