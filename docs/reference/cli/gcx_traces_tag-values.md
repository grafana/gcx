## gcx traces tag-values

List values for a trace tag

### Synopsis

List values for a specific trace tag from a Tempo datasource, optionally filtered by scope and TraceQL query.

```
gcx traces tag-values TAG [flags]
```

### Examples

```

  # List values for a tag (use datasource UID, not name)
  gcx traces tag-values service.name -d <datasource-uid>

  # Filter by scope
  gcx traces tag-values service.name -d <datasource-uid> --scope resource

  # Filter with a TraceQL query
  gcx traces tag-values http.status_code -d <datasource-uid> -q '{ span.http.method = "GET" }'

  # Output as JSON
  gcx traces tag-values service.name -d <datasource-uid> -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.tempo is configured)
  -h, --help                help for tag-values
      --json string         Comma-separated list of fields to include in JSON output, or '?' to discover available fields
  -o, --output string       Output format. One of: json, table, yaml (default "table")
  -q, --query string        TraceQL query to filter tag values
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

