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

Experimental: for large traces, --filter narrows the response to spans matching
a TraceQL spanset filter (V2 only). --keep-hierarchy, --match-depth, and
--ancestor-depth shape how much context around each match is kept, and are
ignored without --filter.

Experimental: --prune collapses repeated sibling spans (for example, a fan-out
of identical DB calls) into a single aggregated span. It takes 'true', 'false',
or 'auto'; bare --prune means true, and omitting it uses the datasource's tenant
default. With --prune=auto the trace is fetched unpruned first and re-requested
with pruning only if it exceeds the agent output budget (100 KiB, overridable
via GCX_AGENT_SPILL_BYTES), which pairs with -o agents for large traces.
--prune-group-by, --prune-min-spans, and --prune-max-parent-depth tune the
pruning behavior and apply whenever pruning is enabled, including by the tenant
default.

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

  # Narrow a large trace to error spans and their ancestor path
  gcx traces get -d UID <trace-id> --filter '{ status = error }' --keep-hierarchy

  # Collapse repeated sibling spans to shrink a huge trace before analysis
  gcx traces get -d UID <trace-id> --prune --llm -o json

  # Prune only if the trace does not fit the agent output budget
  gcx traces get -d UID <trace-id> --prune=auto --llm -o agents
```

### Options

```
      --ancestor-depth int           [experimental] Levels of ancestors to keep above each matched span: -1 = all (default), 0 = none, n = n levels (ignored without --filter or --keep-hierarchy) (default -1)
  -d, --datasource string            Datasource UID (required unless datasources.tempo is configured)
      --filter string                [experimental] TraceQL spanset filter; only matching spans are returned (V2 only)
      --from string                  Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                         help for get
      --jq string                    jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string                  Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --keep-hierarchy               [experimental] Include each matched span's ancestor path to the root (ignored without --filter)
      --llm                          [experimental] Request LLM-friendly trace format by sending the 'Accept: application/vnd.grafana.llm' header. Falls back to default JSON
      --match-depth int              [experimental] Levels of descendants to keep below each matched span: -1 = all, 0 = matched spans only, n = n levels (ignored without --filter)
      --open                         Open the retrieved trace in Grafana Explore
  -o, --output string                Output format. One of: agents, json, table, wide, yaml (default "table")
      --prune string[="true"]        [experimental] Collapse repeated sibling spans (e.g. a fan-out of identical DB calls) into a single aggregated span to shrink large traces: 'true', 'false', or 'auto' to prune only when the unpruned trace exceeds the agent output budget. Bare --prune means true. Overrides the datasource's tenant default; omit to use that default
      --prune-group-by string        [experimental] Comma-separated attribute glob patterns siblings must match to be grouped for pruning, e.g. 'db.*,http.method'. Applies whenever pruning is enabled, including by the datasource's tenant default
      --prune-max-parent-depth int   [experimental] Ancestor levels above pruned leaves that may also be pruned; Tempo defaults to 1. Applies whenever pruning is enabled, including by the datasource's tenant default
      --prune-min-spans int          [experimental] Minimum sibling span count required before a group is pruned; Tempo defaults to 5. Applies whenever pruning is enabled, including by the datasource's tenant default
      --share-link                   Print the Grafana Explore URL for the retrieved trace to stderr
      --since string                 Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --to string                    End time (RFC3339, Unix timestamp, or relative like 'now')
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx traces](gcx_traces.md)	 - Query Tempo datasources and manage Adaptive Traces

