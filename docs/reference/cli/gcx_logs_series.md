## gcx logs series

List log streams

### Synopsis

List log streams (series) from a Loki datasource using LogQL stream selectors. At least one --match selector is required.

```
gcx logs series [flags]
```

### Examples

```

  # List series matching a selector (use datasource UID, not name)
  gcx logs series -d <datasource-uid> --match '{job="varlogs"}'

  # Multiple matchers (OR logic)
  gcx logs series -d <datasource-uid> --match '{job="varlogs"}' --match '{namespace="default"}'

  # Output as JSON
  gcx logs series -d <datasource-uid> --match '{job="varlogs"}' -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless default-loki-datasource is configured)
  -h, --help                help for series
      --json string         Comma-separated list of fields to include in JSON output, or '?' to discover available fields
  -M, --match stringArray   LogQL stream selector (required, e.g., '{job="varlogs"}')
  -o, --output string       Output format. One of: json, table, yaml (default "table")
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx logs](gcx_logs.md)	 - Query Loki datasources and manage Adaptive Logs

