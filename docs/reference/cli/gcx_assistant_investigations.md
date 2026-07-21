## gcx assistant investigations

Manage Grafana Assistant investigations.

### Options

```
  -h, --help   help for investigations
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx assistant](gcx_assistant.md)	 - Interact with Grafana Assistant
* [gcx assistant investigations cancel](gcx_assistant_investigations_cancel.md)	 - Cancel a running investigation.
* [gcx assistant investigations create](gcx_assistant_investigations_create.md)	 - Create a new investigation.
* [gcx assistant investigations get](gcx_assistant_investigations_get.md)	 - Get investigation detail.
* [gcx assistant investigations get-document](gcx_assistant_investigations_get-document.md)	 - Fetch a specific investigation document.
* [gcx assistant investigations list](gcx_assistant_investigations_list.md)	 - List investigations.
* [gcx assistant investigations list-approvals](gcx_assistant_investigations_list-approvals.md)	 - List approval requests for an investigation.
* [gcx assistant investigations list-messages](gcx_assistant_investigations_list-messages.md)	 - List the chat thread messages for a v2 investigation.
* [gcx assistant investigations list-todos](gcx_assistant_investigations_list-todos.md)	 - List agent tasks for an investigation.
* [gcx assistant investigations list-tool-calls](gcx_assistant_investigations_list-tool-calls.md)	 - List tool calls made during a v2 investigation.
* [gcx assistant investigations mode](gcx_assistant_investigations_mode.md)	 - Change autonomy mode of a v2 investigation.
* [gcx assistant investigations narrative](gcx_assistant_investigations_narrative.md)	 - Show the assistant-authored prose for a v2 investigation.
* [gcx assistant investigations pause](gcx_assistant_investigations_pause.md)	 - Pause a running v2 investigation.
* [gcx assistant investigations regenerate-report](gcx_assistant_investigations_regenerate-report.md)	 - Queue regeneration of a v2 investigation report.
* [gcx assistant investigations report](gcx_assistant_investigations_report.md)	 - Show condensed report summary for an investigation.
* [gcx assistant investigations resume](gcx_assistant_investigations_resume.md)	 - Resume a paused v2 investigation.
* [gcx assistant investigations share](gcx_assistant_investigations_share.md)	 - Share a v2 investigation with additional teams.
* [gcx assistant investigations timeline](gcx_assistant_investigations_timeline.md)	 - Show activity timeline for an investigation.

