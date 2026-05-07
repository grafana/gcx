## gcx datasources synthetic-monitoring query

Query a Synthetic Monitoring resource through the datasource

### Synopsis

Query a Synthetic Monitoring resource through the datasource proxy.
SM has no single query verb; pass "probes" or "checks" to target a resource type.
For most use cases, the dedicated subcommands are friendlier:

  gcx datasources synthetic-monitoring probes
  gcx datasources synthetic-monitoring checks

```
gcx datasources synthetic-monitoring query <probes|checks> [flags]
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.synthetic-monitoring is configured)
  -h, --help                help for query
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: json, yaml (default "json")
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

