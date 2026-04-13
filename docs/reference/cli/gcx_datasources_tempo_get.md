## gcx datasources tempo get

Retrieve a trace by ID

### Synopsis

Retrieve a single trace by its trace ID from a Tempo datasource.

TRACE_ID is the hex-encoded trace identifier to retrieve.
Datasource is resolved from -d flag or datasources.tempo in your context.

```
gcx datasources tempo get TRACE_ID [flags]
```

### Examples

```

  # Get a trace using configured default datasource
  gcx datasources tempo get abc123def456

  # Get a trace with explicit datasource UID
  gcx datasources tempo get -d tempo-001 abc123def456

  # Get LLM-friendly output
  gcx datasources tempo get abc123def456 --llm

  # Get a trace within a time range
  gcx datasources tempo get abc123def456 --since 1h
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.tempo is configured)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for get
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --llm                 Request LLM-friendly trace format
  -o, --output string       Output format. One of: json, yaml (default "json")
      --since string        Duration before --to (or now if omitted); mutually exclusive with --from
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx datasources tempo](gcx_datasources_tempo.md)	 - Query Tempo datasources

