## gcx datasources synthetic-monitoring query

Query a Synthetic Monitoring resource through the datasource

### Synopsis

Query a Synthetic Monitoring resource through the datasource proxy.
SM has no single query verb; use one of the resource-typed subcommands.
For most use cases, the dedicated top-level subcommands are friendlier:

  gcx datasources synthetic-monitoring probes
  gcx datasources synthetic-monitoring checks

### Options

```
  -h, --help   help for query
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use (overrides current-context in config)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx datasources synthetic-monitoring](gcx_datasources_synthetic-monitoring.md)	 - Query Synthetic Monitoring datasources
* [gcx datasources synthetic-monitoring query checks](gcx_datasources_synthetic-monitoring_query_checks.md)	 - List Synthetic Monitoring checks
* [gcx datasources synthetic-monitoring query probes](gcx_datasources_synthetic-monitoring_query_probes.md)	 - List Synthetic Monitoring probes

