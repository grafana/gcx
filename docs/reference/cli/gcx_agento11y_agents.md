## gcx agento11y agents

Query Agent Observability agent catalog.

### Options

```
  -h, --help   help for agents
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

* [gcx agento11y](gcx_agento11y.md)	 - Manage Grafana Agent Observability resources
* [gcx agento11y agents get](gcx_agento11y_agents_get.md)	 - Get a single agent definition.
* [gcx agento11y agents list](gcx_agento11y_agents_list.md)	 - List agents.
* [gcx agento11y agents versions](gcx_agento11y_agents_versions.md)	 - List version history for an agent.

