## gcx datasources list

List all datasources

### Synopsis

List all datasources configured in Grafana.

```
gcx datasources list [flags]
```

### Examples

```

	# List all datasources
	gcx datasources list

	# List only Prometheus datasources
	gcx datasources list --type prometheus

	# Output as JSON
	gcx datasources list -o json
```

### Options

```
  -h, --help            help for list
      --json string     Comma-separated list of fields to include in JSON output, or '?' to discover available fields
  -o, --output string   Output format. One of: json, table, yaml (default "table")
  -t, --type string     Filter by datasource type (e.g., prometheus, loki)
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

* [gcx datasources](gcx_datasources.md)	 - Manage and query Grafana datasources

