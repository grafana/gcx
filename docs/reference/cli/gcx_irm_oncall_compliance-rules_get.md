## gcx irm oncall compliance-rules get

Show the org's notification compliance rules.

```
gcx irm oncall compliance-rules get [flags]
```

### Options

```
  -h, --help                  help for get
      --instance-id string    TEST-ONLY: X-Grafana-Instance-ID for direct transport. Defaults to $INSTANCE_ID.
      --jq string             jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string           Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --oncall-token string   TEST-ONLY: OnCall API token for direct transport. Defaults to $ONCALL_TOKEN.
      --oncall-url string     TEST-ONLY: OnCall engine base URL (e.g. http://host:8084); bypasses the plugin proxy. Defaults to $ONCALL_URL.
  -o, --output string         Output format. One of: agents, json, text, yaml (default "text")
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

* [gcx irm oncall compliance-rules](gcx_irm_oncall_compliance-rules.md)	 - Manage the org's notification compliance rules (expected configuration).

