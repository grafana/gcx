## gcx traces baseline

[experimental] Find same-operation trace candidates.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

Find unranked candidates when you have a seed trace (TRACE_ID) but need a useful
comparison; if you already have both trace IDs, use 'gcx traces diff' directly.

Retrieval fetches the seed, matches its root service/operation, requires root
status != error (including unset), retains downstream errors, and pins up to
three busiest downstream services; these heuristics do not prove health or
comparable work.

Apply verified environment, tenant, or operation constraints with --filter from
the first request, then fetch a small batch of plausible candidates with
'gcx traces get --llm' when their summaries lack enough context.

Use candidate bodies and exploratory diffs to accept or reject controls for the
actual symptom, rather than requiring exact workload filters before the first
diff or treating the first result as healthy.

```
gcx traces baseline TRACE_ID [flags]
```

### Examples

```

  # Find a small shortlist; COHORT is an already-verified TraceQL spanset
  gcx traces baseline --context prod -d UID <seed-id> --filter "$COHORT" --limit 5

  # With no additional known scope, use the seed-derived defaults
  gcx traces baseline --context prod -d UID <seed-id> --limit 5

  # Inspect selected candidates; repeat for a small batch, not every result
  gcx traces get --context prod -d UID <candidate-id> --llm -o agents

  # Assess a candidate with an exploratory diff (seed minus candidate)
  gcx traces diff --context prod -d UID <candidate-id> <seed-id>

  # Set the candidate window; this does not bound the seed trace lookup
  gcx traces baseline --context prod -d UID <seed-id> --filter "$COHORT" --limit 5 \
    --from 2026-01-15T08:00:00Z --to 2026-01-15T09:00:00Z
```

### Options

```
  -d, --datasource string    Datasource UID (required unless datasources.tempo is configured)
      --filter stringArray   Raw TraceQL spanset expression to constrain candidates (repeatable; ANDed server-side with the generated query)
      --from string          Absolute start time override (RFC3339, Unix timestamp, or relative like 'now-1h'); requires --to
  -h, --help                 help for baseline
      --jq string            jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string          Comma-separated list of dotted field paths to include in JSON output (e.g. spec.name), or 'list' (or '?') to discover the available paths
      --limit int            Maximum number of candidates to return; must be at least 1 (default 20)
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

