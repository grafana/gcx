## gcx irm oncall shifts update

Update a shift by ID.

```
gcx irm oncall shifts update <id> [flags]
```

### Options

```
  -f, --filename string   File containing the resource definition (JSON/YAML, use - for stdin)
  -h, --help              help for update
      --json string       Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string     Output format. One of: agents, json, yaml (default "yaml")
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

* [gcx irm oncall shifts](gcx_irm_oncall_shifts.md)	 - Manage OnCall shifts.

