## gcx irm oncall integrations start-maintenance

Start maintenance on an integration.

### Synopsis

Start maintenance on an integration.

Maintenance suppresses escalation during planned work. Mode "maintenance"
groups every alert of the integration into one alert group and pages nobody.
Mode "debug" routes each alert to its author only.

The backend accepts these durations only: 3600, 10800, 21600, 43200, or 86400
seconds. These values are 1, 3, 6, 12, and 24 hours.

```
gcx irm oncall integrations start-maintenance <id> [flags]
```

### Options

```
      --duration int    Maintenance duration in seconds (3600, 10800, 21600, 43200, 86400) (default 3600)
  -h, --help            help for start-maintenance
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --mode string     Maintenance mode (debug, maintenance) (default "maintenance")
  -o, --output string   Output format. One of: agents, json, text, yaml (default "text")
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

