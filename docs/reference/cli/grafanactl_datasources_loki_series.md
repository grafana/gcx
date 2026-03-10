## grafanactl datasources loki series

List log streams

### Synopsis

List log streams (series) from a Loki datasource using LogQL stream selectors. At least one --match selector is required.

```
grafanactl datasources loki series [flags]
```

### Examples

```

	# List series matching a selector (use datasource UID, not name)
	grafanactl datasources loki series -d <datasource-uid> --match '{job="varlogs"}'

	# Match with regex and multiple labels
	grafanactl datasources loki series -d <datasource-uid> --match '{container_name=~"prometheus.*", component="server"}'

	# Multiple matchers (OR logic)
	grafanactl datasources loki series -d <datasource-uid> --match '{job="varlogs"}' --match '{namespace="default"}'

	# Output as JSON
	grafanactl datasources loki series -d <datasource-uid> --match '{job="varlogs"}' -o json
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
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GRAFANACTL_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [grafanactl datasources loki](grafanactl_datasources_loki.md)	 - Loki datasource operations

