## gcx irm incidents get-pir

Get the post-incident review (PIR) document URL for an incident.

### Synopsis

Resolve the post-incident review (PIR) document URL for an incident.

The URL is not carried in the incident payload. PIRs are optional and only the
Google Workspace integration creates them, so the link exists only where that
integration ran. It is recorded on the hook run that copied the PIR template,
which this command reads and resolves.

An incident can have more than one PIR document if the template was copied
again; the most recently created one is reported.

An incident without a PIR document prints nothing and exits 0.

```
gcx irm incidents get-pir <incident-id> [flags]
```

### Options

```
  -h, --help            help for get-pir
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, text, yaml (default "text")
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx irm incidents](gcx_irm_incidents.md)	 - Manage incidents.

