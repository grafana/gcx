## gcx traces tags

List trace tag names

### Synopsis

List all trace tag names from a Tempo datasource, optionally filtered by scope and TraceQL query.

```
gcx traces tags [flags]
```

### Examples

```

  # List all tags (use datasource UID, not name)
  gcx traces tags -d <datasource-uid>

  # List tags for a specific scope
  gcx traces tags -d <datasource-uid> --scope resource

  # Filter tags with a TraceQL query
  gcx traces tags -d <datasource-uid> -q '{ span.http.status_code >= 500 }'

  # Output as JSON
  gcx traces tags -d <datasource-uid> -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.tempo is configured)
  -h, --help                help for tags
      --json string         Comma-separated list of fields to include in JSON output, or '?' to discover available fields
  -o, --output string       Output format. One of: json, table, yaml (default "table")
  -q, --query string        TraceQL query to filter tags
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

