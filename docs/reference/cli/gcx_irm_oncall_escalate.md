## gcx irm oncall escalate

Create a direct escalation.

### Synopsis

Page users or a team. Use --incident-id to associate the page with an existing incident.
Incident participants, timeline activity, and context update asynchronously;
do not repeat a successful page while waiting for those updates.

```
gcx irm oncall escalate [flags]
```

### Examples

```
  # Page a user
  gcx irm oncall escalate --title "Database outage" --user-ids U123

  # Page a team for an incident
  gcx irm oncall escalate --title "Database outage" --team T123 --incident-id INC-123

  # Send an important page to multiple users for an incident
  gcx irm oncall escalate --title "Database outage" --user-ids U123,U456 --important --incident-id INC-123
```

### Options

```
  -h, --help                 help for escalate
      --important            Mark as important
      --incident-id string   Incident ID to associate with the page
      --jq string            jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --message string       Message for the escalation
  -o, --output string        Output format. One of: agents, json, text, yaml (default "text")
      --team string          Team ID
      --title string         Title of the escalation (required)
      --user-ids strings     User IDs (comma-separated)
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx irm oncall](gcx_irm_oncall.md)	 - Manage Grafana OnCall resources.

