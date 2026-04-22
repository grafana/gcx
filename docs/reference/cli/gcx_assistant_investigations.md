## gcx assistant investigations

Manage Grafana Assistant investigations.

### Options

```
  -h, --help   help for investigations
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

* [gcx assistant](gcx_assistant.md)	 - Interact with Grafana Assistant
* [gcx assistant investigations approvals](gcx_assistant_investigations_approvals.md)	 - List approval requests for an investigation.
* [gcx assistant investigations cancel](gcx_assistant_investigations_cancel.md)	 - Cancel a running investigation.
* [gcx assistant investigations create](gcx_assistant_investigations_create.md)	 - Create a new investigation.
* [gcx assistant investigations document](gcx_assistant_investigations_document.md)	 - Fetch a specific investigation document.
* [gcx assistant investigations get](gcx_assistant_investigations_get.md)	 - Get investigation detail.
* [gcx assistant investigations list](gcx_assistant_investigations_list.md)	 - List investigations.
* [gcx assistant investigations report](gcx_assistant_investigations_report.md)	 - Show condensed report summary for an investigation.
* [gcx assistant investigations timeline](gcx_assistant_investigations_timeline.md)	 - Show activity timeline for an investigation.
* [gcx assistant investigations todos](gcx_assistant_investigations_todos.md)	 - Show agent tasks for an investigation.

