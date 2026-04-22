## gcx traces adaptive

Manage Adaptive Traces resources

### Options

```
  -h, --help   help for adaptive
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --limit int          Maximum number of items to return from list operations (0 for all; defaults to 50 in agent mode)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx traces](gcx_traces.md)	 - Query Tempo datasources and manage Adaptive Traces
* [gcx traces adaptive policies](gcx_traces_adaptive_policies.md)	 - Manage Adaptive Traces sampling policies.
* [gcx traces adaptive recommendations](gcx_traces_adaptive_recommendations.md)	 - Manage Adaptive Traces recommendations.

