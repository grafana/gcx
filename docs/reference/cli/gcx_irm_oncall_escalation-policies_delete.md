## gcx irm oncall escalation-policies delete

Delete an escalation policy by ID.

```
gcx irm oncall escalation-policies delete <id> [flags]
```

### Options

```
      --force   Skip confirmation prompt
  -h, --help    help for delete
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

* [gcx irm oncall escalation-policies](gcx_irm_oncall_escalation-policies.md)	 - Manage escalation policies.

