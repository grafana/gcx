## gcx vulnobs groups

Manage vulnerability-obs groups (read-only).

### Options

```
  -h, --help   help for groups
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

* [gcx vulnobs](gcx_vulnobs.md)	 - Inspect Grafana Vulnerability Observability data (read-only).
* [gcx vulnobs groups list](gcx_vulnobs_groups_list.md)	 - List all vulnerability-obs groups.

