## gcx stacks

Manage Grafana Cloud stacks (list, create, update, delete)

### Options

```
      --config string   Path to the configuration file to use
  -h, --help            help for stacks
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
* [gcx stacks create](gcx_stacks_create.md)	 - Create a new Grafana Cloud stack.
* [gcx stacks delete](gcx_stacks_delete.md)	 - Delete a Grafana Cloud stack.
* [gcx stacks get](gcx_stacks_get.md)	 - Get details of a single stack.
* [gcx stacks list](gcx_stacks_list.md)	 - List stacks in an organisation.
* [gcx stacks regions](gcx_stacks_regions.md)	 - List available regions for stack creation.
* [gcx stacks update](gcx_stacks_update.md)	 - Update a Grafana Cloud stack.

