## gcx appo11y operations

Rank and inspect operations (span names) across App Observability services

### Options

```
  -h, --help   help for operations
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

* [gcx appo11y](gcx_appo11y.md)	 - Manage Grafana App Observability settings
* [gcx appo11y operations get](gcx_appo11y_operations_get.md)	 - Inspect a single operation (span name) within one service: RED snapshot + its share of the service's time.
* [gcx appo11y operations list](gcx_appo11y_operations_list.md)	 - Rank operations (span names) fleet-wide by time share, across every App Observability service.

