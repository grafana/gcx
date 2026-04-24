## gcx traces get

Retrieve a trace by ID

### Synopsis

Retrieve a single trace by its trace ID from a Tempo datasource.

TRACE_ID is the hex-encoded trace identifier to retrieve.
Datasource is resolved from -d flag or datasources.tempo in your context.
Use --share-link to print a Grafana Explore URL for the trace, or --open to
open it in your browser after retrieval succeeds. Share links require an
explicit time range via --since or --from/--to.

```
gcx traces get TRACE_ID [flags]
```

### Examples

```

  # Fetch a trace by ID
  gcx traces get -d UID <trace-id>

  # Print a Grafana Explore share link for the trace
  gcx traces get -d UID <trace-id> --share-link

  # Output as JSON
  gcx traces get -d UID <trace-id> -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.tempo is configured)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for get
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --llm                 Request LLM-friendly trace format
      --open                Open the retrieved trace in Grafana Explore
  -o, --output string       Output format. One of: json, yaml (default "json")
      --share-link          Print the Grafana Explore URL for the retrieved trace to stderr
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

* [gcx traces](gcx_traces.md)	 - Query Tempo datasources and manage Adaptive Traces

