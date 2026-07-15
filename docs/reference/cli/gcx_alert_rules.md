## gcx alert rules

Inspect alert rule state and health.

### Synopsis

Inspect Grafana-managed alert rules via the Prometheus-compatible status API.

These commands are read-only: they show evaluation state, health, and timing.
To create, modify, or delete alert rules, use the resources tier:

  gcx resources pull alertrules -p ./rules   # export rules to disk
  gcx resources push -p ./rules              # apply edited rules
  gcx resources delete alertrules/<name>     # delete a rule

### Options

```
  -h, --help   help for rules
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

* [gcx alert](gcx_alert.md)	 - Inspect alert rule status and manage notification settings
* [gcx alert rules get](gcx_alert_rules_get.md)	 - Get a single alert rule.
* [gcx alert rules list](gcx_alert_rules_list.md)	 - List alert rules.

