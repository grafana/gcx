## gcx traces get

Retrieve a trace by ID

### Synopsis

Retrieve a single trace by its trace ID from a Tempo datasource.

TRACE_ID is the hex-encoded trace identifier to retrieve.
Datasource is resolved from -d flag or datasources.tempo in your context.
Use --share-link to print a Grafana Explore URL for the trace, or --open to
open it in your browser after retrieval succeeds. Share links require an
explicit time range via --since or --from/--to.

Experimental: --llm requests the trace in a new LLM-friendly JSON format by
sending the "Accept: application/vnd.grafana.llm" header. Datasources that do
not support this format return the standard response.

```
gcx traces get TRACE_ID [flags]
```

### Examples

```

  # Fetch a trace by ID for agent analysis
  gcx traces get -d UID <trace-id> --llm -o json

  # Print a Grafana Explore share link for the trace
  gcx traces get -d UID <trace-id> --share-link

  # Output raw OTLP-shaped JSON when explicitly needed
  gcx traces get -d UID <trace-id> -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.tempo is configured)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for get
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --llm                 [experimental] Request LLM-friendly trace format by sending the 'Accept: application/vnd.grafana.llm' header. Falls back to default JSON
      --open                Open the retrieved trace in Grafana Explore
  -o, --output string       Output format. One of: agents, json, table, wide, yaml (default "table")
      --share-link          Print the Grafana Explore URL for the retrieved trace to stderr
      --since string        Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx traces](gcx_traces.md)	 - Query Tempo datasources and manage Adaptive Traces

