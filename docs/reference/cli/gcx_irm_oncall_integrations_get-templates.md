## gcx irm oncall integrations get-templates

Get the alert templates of an integration.

### Synopsis

Get the alert templates of an integration.

The templates decide what a responder reads and hears: the alert title, the
message, the grouping identifier, the resolve condition, and a separate
rendering for each channel (web, phone call, Short Message Service, email,
Slack, and Microsoft Teams).

The command emits the whole template document. Edit that document, then pass
it back through set-templates.

```
gcx irm oncall integrations get-templates <id> [flags]
```

### Options

```
  -h, --help            help for get-templates
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, yaml (default "yaml")
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

* [gcx irm oncall integrations](gcx_irm_oncall_integrations.md)	 - Manage OnCall integrations.

