## gcx agent

Agent mode utilities

### Synopsis

Utilities for gcx agent mode: manage spill files, install and update Agent Skills, and other agent session housekeeping.

### Options

```
  -h, --help   help for agent
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
* [gcx agent prune](gcx_agent_prune.md)	 - Remove gcx agent spill files older than 30 minutes
* [gcx agent skills](gcx_agent_skills.md)	 - Manage portable gcx Agent Skills

