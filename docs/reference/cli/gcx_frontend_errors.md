## gcx frontend errors

Triage Frontend Observability error groups.

### Options

```
  -h, --help   help for errors
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

* [gcx frontend](gcx_frontend.md)	 - Manage Grafana Frontend Observability resources
* [gcx frontend errors get](gcx_frontend_errors_get.md)	 - Show one error group: stack frames, first-seen release, breakdowns, and recent occurrences.
* [gcx frontend errors list](gcx_frontend_errors_list.md)	 - List error groups for a Frontend Observability app.

