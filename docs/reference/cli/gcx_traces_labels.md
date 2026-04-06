## gcx traces labels

List trace labels or label values

### Synopsis

List all trace labels or get values for a specific label from a Tempo datasource.

When -l/--label is provided, returns values for that label.
When -l is omitted, returns all label names.

Datasource is resolved from -d flag or datasources.tempo in your context.

```
gcx traces labels [flags]
```

### Examples

```

  # List all labels
  gcx traces labels -d <datasource-uid>

  # Get values for a specific label
  gcx traces labels -d <datasource-uid> -l service.name

  # Using the tags alias
  gcx traces tags -d <datasource-uid> -l service.name

  # Filter by scope
  gcx traces labels -d <datasource-uid> -l service.name --scope span

  # Filter with a TraceQL query
  gcx traces labels -d <datasource-uid> -q '{ span.http.status_code >= 500 }'

  # Output as JSON
  gcx traces labels -d <datasource-uid> -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.tempo is configured)
  -h, --help                help for labels
      --json string         Comma-separated list of fields to include in JSON output, or '?' to discover available fields
  -l, --label string        Get values for this label (omit to list all labels)
  -o, --output string       Output format. One of: json, table, yaml (default "table")
  -q, --query string        TraceQL query to filter labels
      --scope string        Tag scope filter (resource, span, event, link, instrumentation)
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

* [gcx traces](gcx_traces.md)	 - Query Tempo datasources and manage Adaptive Traces

