## gcx datasources tempo get

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

Experimental: for large traces, --q narrows the response to spans matching a
TraceQL spanset filter (V2 only). --keep-hierarchy, --match-depth, and
--ancestor-depth shape how much context around each match is kept, and are
ignored without --q. --span-pruning collapses repeated sibling spans (for
example, a fan-out of identical DB calls) into a single aggregated span;
--span-pruning-group-by, --span-pruning-min-spans, and
--span-pruning-max-parent-depth tune that behavior and are ignored without
--span-pruning.

```
gcx datasources tempo get TRACE_ID [flags]
```

### Examples

```

  # Get LLM-friendly output for agent analysis
  gcx datasources tempo get abc123def456 --llm -o json

  # Get LLM-friendly output with explicit datasource UID
  gcx datasources tempo get -d tempo-001 abc123def456 --llm -o json

  # Print a Grafana Explore share link for the trace
  gcx datasources tempo get abc123def456 --share-link

  # Get a human-readable trace table
  gcx datasources tempo get abc123def456

  # Get LLM-friendly output within a time range
  gcx datasources tempo get abc123def456 --since 1h --llm -o json

  # Narrow a large trace to error spans and their ancestor path
  gcx datasources tempo get abc123def456 --q '{ status = error }' --keep-hierarchy

  # Collapse repeated sibling spans to shrink a huge trace before analysis
  gcx datasources tempo get abc123def456 --span-pruning --llm -o json
```

### Options

```
      --ancestor-depth int                  [experimental] Levels of ancestors to keep above each matched span: -1 = all (default), 0 = none, n = n levels (ignored without --q or --keep-hierarchy) (default -1)
  -d, --datasource string                   Datasource UID (required unless datasources.tempo is configured)
      --from string                         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                                help for get
      --jq string                           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string                         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --keep-hierarchy                      [experimental] Include each matched span's ancestor path to the root (ignored without --q)
      --llm                                 [experimental] Request LLM-friendly trace format by sending the 'Accept: application/vnd.grafana.llm' header. Falls back to default JSON
      --match-depth int                     [experimental] Levels of descendants to keep below each matched span: -1 = all, 0 = matched spans only, n = n levels (ignored without --q)
      --open                                Open the retrieved trace in Grafana Explore
  -o, --output string                       Output format. One of: agents, json, table, wide, yaml (default "table")
      --q string                            [experimental] TraceQL spanset filter; only matching spans are returned (V2 only)
      --share-link                          Print the Grafana Explore URL for the retrieved trace to stderr
      --since string                        Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --span-pruning                        [experimental] Collapse repeated sibling spans (e.g. a fan-out of identical DB calls) into a single aggregated span to shrink large traces. Overrides the datasource's tenant default; omit to use that default
      --span-pruning-group-by string        [experimental] Comma-separated attribute glob patterns siblings must match to be grouped for pruning, e.g. 'db.*,http.method' (ignored without --span-pruning)
      --span-pruning-max-parent-depth int   [experimental] Ancestor levels above pruned leaves that may also be pruned; Tempo defaults to 1 (ignored without --span-pruning)
      --span-pruning-min-spans int          [experimental] Minimum sibling span count required before a group is pruned; Tempo defaults to 5 (ignored without --span-pruning)
      --to string                           End time (RFC3339, Unix timestamp, or relative like 'now')
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

* [gcx datasources tempo](gcx_datasources_tempo.md)	 - Query Tempo datasources

