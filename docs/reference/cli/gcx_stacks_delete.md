## gcx stacks delete

Delete a Grafana Cloud stack.

### Synopsis

Delete a Grafana Cloud stack.

This command permanently deletes a Grafana Cloud stack and ALL its data
(dashboards, alerts, datasources, metrics, logs, traces). This action is
IRREVERSIBLE. Always confirm with the user by name before executing. Prefer
--dry-run first. Never run this command without explicit user confirmation.

```
gcx stacks delete <stack-slug> [flags]
```

### Options

```
      --dry-run   Preview the operation without executing it
  -h, --help      help for delete
  -y, --yes       Skip confirmation prompt
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use (overrides current-context in config)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx stacks](gcx_stacks.md)	 - Manage Grafana Cloud stacks (list, create, update, delete)

