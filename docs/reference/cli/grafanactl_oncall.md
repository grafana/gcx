## grafanactl oncall

Manage Grafana OnCall resources.

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for oncall
```

### Options inherited from parent commands

```
      --agent           Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GRAFANACTL_AGENT_MODE env vars.
      --no-color        Disable color output
      --no-truncate     Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count   Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [grafanactl](grafanactl.md)	 - 
* [grafanactl oncall alert-groups](grafanactl_oncall_alert-groups.md)	 - Manage alert groups.
* [grafanactl oncall escalate](grafanactl_oncall_escalate.md)	 - Create a direct escalation.
* [grafanactl oncall final-shifts](grafanactl_oncall_final-shifts.md)	 - List final shifts for a schedule.
* [grafanactl oncall get](grafanactl_oncall_get.md)	 - Get a single OnCall resource by ID.
* [grafanactl oncall list](grafanactl_oncall_list.md)	 - List OnCall resources.
* [grafanactl oncall users](grafanactl_oncall_users.md)	 - Manage OnCall users.

