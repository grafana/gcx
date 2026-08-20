## gcx traces baseline

[experimental] Find healthy baseline candidates for a trace

### Synopsis

[experimental] Find healthy, same-operation candidate traces to compare against a seed trace.

TRACE_ID is the seed trace (typically a faulty one). The command fetches it,
reads its root service/operation and its busiest downstream services, then
searches for traces with the same root identity whose operation succeeded
(status != error), pinned to the seed's top downstream services so candidates
stay on the same execution path.

Run the generated query without --filter first and inspect candidates with
'gcx traces diff <candidate> <seed>'. Only when those candidates are not valid
comparisons, add repeatable --filter expressions to require domain-specific
context such as a tenant, cluster, or query path. Filters are trusted raw
TraceQL, ANDed with the generated query, and syntax-validated by Tempo.

Downstream errors are deliberately NOT filtered out: surfacing them is the job
of 'gcx traces diff <candidate> <seed>', which is the real similarity and
root-cause step. This command only retrieves candidate trace IDs (in the order
search returns them); it does not rank them.

By default candidates are searched within the seed trace's own time range,
padded by --window (30m) on each side, so candidates from before or after the
seed are eligible. Widen with --window, or set an absolute window with
--from/--to.

This is a heuristic retrieval built on TraceQL search; its query and output may
change.

```
gcx traces baseline TRACE_ID [flags]
```

### Examples

```

  # Start unfiltered, then diff a candidate as the baseline (B - A semantics)
  gcx traces baseline <trace-id>
  gcx traces diff <candidate> <trace-id>

  # Only if unfiltered candidates are not valid comparisons, refine by tenant
  gcx traces baseline <trace-id> --filter '{ span.tenantID = "tenant-a" }'

  # Widen the window to 6h before and after the seed, output JSON
  gcx traces baseline <trace-id> --window 6h -o json
```

### Options

```
  -d, --datasource string    Datasource UID (required unless datasources.tempo is configured)
      --filter stringArray   Raw TraceQL spanset expression to refine candidates when unfiltered results are not valid comparisons (repeatable; ANDed with the generated query)
      --from string          Absolute start time override (RFC3339, Unix timestamp, or relative like 'now-1h'); requires --to
  -h, --help                 help for baseline
      --jq string            jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int            Maximum number of candidates to return (default 20)
  -o, --output string        Output format. One of: agents, json, table, wide, yaml (default "table")
      --to string            Absolute end time override (RFC3339, Unix timestamp, or relative like 'now'); requires --from
      --window string        Search window padding applied before and after the seed trace's time range, so candidates from before or after the seed are eligible (e.g., 30m, 6h, 7d). Ignored when --from/--to are set (default "30m")
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

