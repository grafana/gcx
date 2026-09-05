## gcx irm oncall escalation-policies update-position

Update the position of an escalation step in its chain.

### Synopsis

Update the position of an escalation step in its chain.

An escalation chain runs its steps in order, so the position decides when a
step fires. The position is zero-based: 0 is the first step. The backend
renumbers the other steps of the chain.

This command is the only way to set the order. The backend does not report the
position of a step or of a route, so a caller cannot read the current order
back.

```
gcx irm oncall escalation-policies update-position <id> [flags]
```

### Options

```
  -h, --help            help for update-position
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, text, yaml (default "text")
      --position int    Zero-based target position (required)
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

* [gcx irm oncall escalation-policies](gcx_irm_oncall_escalation-policies.md)	 - Manage escalation policies.

