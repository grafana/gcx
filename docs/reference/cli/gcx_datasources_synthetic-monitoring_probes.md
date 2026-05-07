## gcx datasources synthetic-monitoring probes

List Synthetic Monitoring probes

### Synopsis

List all probes accessible through the configured Synthetic Monitoring datasource.

```
gcx datasources synthetic-monitoring probes [flags]
```

### Examples

```

  # List probes (use datasource UID, not name)
  gcx datasources synthetic-monitoring probes -d UID

  # Output as JSON
  gcx datasources synthetic-monitoring probes -d UID -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.synthetic-monitoring is configured)
  -h, --help                help for probes
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

