## gcx loki labels

List labels or label values

### Synopsis

List all labels or get values for a specific label from a Loki datasource.

```
gcx loki labels [flags]
```

### Examples

```

	# List all labels (use datasource UID, not name)
	gcx loki labels -d <datasource-uid>

	# Get values for a specific label
	gcx loki labels -d <datasource-uid> --label job

	# Output as JSON
	gcx loki labels -d <datasource-uid> -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless default-loki-datasource is configured)
  -h, --help                help for labels
      --json string         Comma-separated list of fields to include in JSON output, or '?' to discover available fields
  -l, --label string        Get values for this label (omit to list all labels)
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

* [gcx loki](gcx_loki.md)	 - Loki datasource operations

