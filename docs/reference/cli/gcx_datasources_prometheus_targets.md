## gcx datasources prometheus targets

List scrape targets

### Synopsis

List scrape targets from a Prometheus datasource.

```
gcx datasources prometheus targets [flags]
```

### Examples

```

	# List active targets (use datasource UID, not name)
	gcx datasources prometheus targets -d <datasource-uid>

	# List dropped targets
	gcx datasources prometheus targets -d <datasource-uid> --state dropped

	# List all targets
	gcx datasources prometheus targets -d <datasource-uid> --state any

	# Output as JSON
	gcx datasources prometheus targets -d <datasource-uid> -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless default-prometheus-datasource is configured)
  -h, --help                help for targets
      --json string         Comma-separated list of fields to include in JSON output, or '?' to discover available fields
  -o, --output string       Output format. One of: json, table, yaml (default "table")
      --state string        Filter by target state: active, dropped, any (default: active)
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

