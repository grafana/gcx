## gcx irm oncall webhooks list-triggers

List allowed values for a webhook's trigger_type field.

### Synopsis

List the trigger types that an outgoing webhook accepts. The command reads the catalog from the Incident Response and Management backend, so the values match your stack. Put the numeric value in the trigger_type field of a webhook manifest.

```
gcx irm oncall webhooks list-triggers [flags]
```

### Examples

```
  # List the trigger types that a webhook accepts
  gcx irm oncall webhooks list-triggers

  # Read the numeric value of one trigger type
  gcx irm oncall webhooks list-triggers -o json | jq -r '.[] | select(.display_name == "<display-name>") | .value'

  # Put that value in the trigger_type field of webhook.yaml, then create the webhook
  gcx irm oncall webhooks create -f webhook.yaml
```

### Options

```
  -h, --help            help for list-triggers
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, table, yaml (default "table")
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

* [gcx irm oncall webhooks](gcx_irm_oncall_webhooks.md)	 - Manage outgoing webhooks.

