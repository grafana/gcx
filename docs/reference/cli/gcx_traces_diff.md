## gcx traces diff

[experimental] Compare execution of two traces.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

Compare two known trace IDs using the Grafana Cloud-only trace-diff API;
use 'gcx traces baseline' first only when you need candidate IDs.

TRACE_A is the prospective baseline and TRACE_B is the comparison, with deltas
B - A: positive duration means B is slower and negative means faster, not
automatically a regression or improvement (a request can fail early).

Use candidate bodies and exploratory diffs to assess comparability, rejecting
obvious context mismatches first and interpreting timing at the affected request
boundary; a diff localizes execution changes but does not establish their cause.

Keep the same context/datasource and make --from/--to (or --since) cover both
executions; without time bounds the lookup uses the full lookback, and if the
endpoint is unavailable, use 'gcx traces get --llm' to compare both bodies
manually instead.

```
gcx traces diff TRACE_A TRACE_B [flags]
```

### Examples

```

  # Compare an already-known pair directly; baseline search is not required
  gcx traces diff --context prod -d UID <baseline-id> <comparison-id>

  # Inspect a plausible candidate before using a diff to assess it
  gcx traces get --context prod -d UID <candidate-id> --llm -o agents

  # Assess selected candidates against the seed; repeat only as useful
  gcx traces diff --context prod -d UID <candidate-id> <seed-id>

  # Bound BOTH trace lookups, including an older candidate, and allow spilling
  gcx traces diff --context prod -d UID <candidate-id> <seed-id> \
    --from 2026-01-15T08:00:00Z --to 2026-01-15T10:00:00Z -o agents
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.tempo is configured)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for diff
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of dotted field paths to include in JSON output (e.g. spec.name), or 'list' (or '?') to discover the available paths
  -o, --output string       Output format. One of: agents, json, yaml (default "json")
      --since string        Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
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

