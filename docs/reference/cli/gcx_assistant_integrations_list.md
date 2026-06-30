## gcx assistant integrations list

List integrations.

### Synopsis

List integrations with optional scope and status filters.

```
gcx assistant integrations list [flags]
```

### Examples

```
  gcx assistant integrations list
  gcx assistant integrations list --scope=user
  gcx assistant integrations list --enabled-only --limit=50
  gcx assistant integrations list -o json
```

### Options

```
      --enabled-only    Only return enabled integrations
  -h, --help            help for list
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int       Maximum number of integrations to return (default 20)
      --offset int      Number of integrations to skip (for pagination)
  -o, --output string   Output format. One of: agents, json, table, wide, yaml (default "table")
      --scope string    Filter by scope (user or tenant)
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

* [gcx assistant integrations](gcx_assistant_integrations.md)	 - Manage Grafana Assistant integrations.

