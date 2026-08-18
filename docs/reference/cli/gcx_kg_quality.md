## gcx kg quality

Inspect Knowledge Graph entity quality reports.

### Synopsis

Inspect Knowledge Graph entity quality reports.

Quality reports grade how well an entity is instrumented — each report is a set
of checks (request metrics, service-map data, deployment environment, span
cardinality, logs, profiles, ...) with an overall quality percentage. Use
'list' to rank entities by quality, and 'get' to see the failing checks and
remediation links for one entity.

### Options

```
  -h, --help   help for quality
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

* [gcx kg](gcx_kg.md)	 - Manage Grafana Knowledge Graph rules, entities, and insights
* [gcx kg quality get](gcx_kg_quality_get.md)	 - Get the full quality report for a single entity.
* [gcx kg quality list](gcx_kg_quality_list.md)	 - List entity quality reports, ranked by quality percent.

