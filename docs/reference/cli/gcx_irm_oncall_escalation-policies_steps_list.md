## gcx irm oncall escalation-policies steps list

List allowed values for an escalation policy's step field.

### Synopsis

List the step types that an escalation policy accepts. The command reads the catalog from the Incident Response and Management backend, so the values match your stack. Put the numeric value in the step field of an escalation policy manifest.

```
gcx irm oncall escalation-policies steps list [flags]
```

### Examples

```
  # List the step types that an escalation policy accepts
  gcx irm oncall escalation-policies list-step-types

  # Read the numeric value of one step type
  gcx irm oncall escalation-policies list-step-types -o json | jq -r '.[] | select(.display_name == "<display-name>") | .value'

  # Put that value in the step field of policy.yaml, then create the policy
  gcx irm oncall escalation-policies create -f policy.yaml
```

### Options

```
  -h, --help            help for list
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, table, yaml (default "table")
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

* [gcx irm oncall escalation-policies steps](gcx_irm_oncall_escalation-policies_steps.md)	 - Discover allowed escalation policy step types.

