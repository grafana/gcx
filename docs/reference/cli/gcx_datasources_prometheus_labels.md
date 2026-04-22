## gcx datasources prometheus labels

List labels or label values

### Synopsis

List all labels or get values for a specific label from a Prometheus datasource.

```
gcx datasources prometheus labels [flags]
```

### Examples

```

	# List all labels (use datasource UID, not name)
	gcx datasources prometheus labels -d UID

	# Get values for a specific label
	gcx datasources prometheus labels -d UID --label job

	# Output as JSON
	gcx datasources prometheus labels -d UID -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless default-prometheus-datasource is configured)
  -h, --help                help for labels
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -l, --label string        Get values for this label (omit to list all labels)
  -o, --output string       Output format. One of: json, table, yaml (default "table")
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --limit int          Maximum number of items to return from list operations (0 for all; defaults to 50 in agent mode)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx datasources prometheus](gcx_datasources_prometheus.md)	 - Query Prometheus datasources

