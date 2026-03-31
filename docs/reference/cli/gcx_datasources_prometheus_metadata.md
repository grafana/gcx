## gcx datasources prometheus metadata

Get metric metadata

### Synopsis

Get metadata (type, help text) for metrics from a Prometheus datasource.

```
gcx datasources prometheus metadata [flags]
```

### Examples

```

	# Get all metric metadata (use datasource UID, not name)
	gcx datasources prometheus metadata -d <datasource-uid>

	# Get metadata for a specific metric
	gcx datasources prometheus metadata -d <datasource-uid> --metric http_requests_total

	# Output as JSON
	gcx datasources prometheus metadata -d <datasource-uid> -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless default-prometheus-datasource is configured)
  -h, --help                help for metadata
      --json string         Comma-separated list of fields to include in JSON output, or '?' to discover available fields
  -m, --metric string       Filter by metric name
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

* [gcx datasources prometheus](gcx_datasources_prometheus.md)	 - Prometheus datasource operations

