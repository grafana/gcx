## gcx annotations list

List annotations (last 24h by default).

### Synopsis

List annotations from Grafana.

By default, returns only annotations from the last 24 hours. Use --lookback
to widen the window, or --from/--to for an explicit time range (epoch ms).

```
gcx annotations list [flags]
```

### Examples

```
  gcx annotations list
  gcx annotations list --lookback 168h
  gcx annotations list --tags deploy,prod
  gcx annotations list --limit 20
```

### Options

```
      --from int            Start time in epoch milliseconds
  -h, --help                help for list
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int           Maximum results to return (0 = unlimited) (default 100)
      --lookback duration   Lookback duration (e.g. 24h, 48h, 7d); ignored if --from/--to are set (default 24h0m0s)
  -o, --output string       Output format. One of: json, table, yaml (default "table")
      --tags strings        Filter by tags (comma-separated or repeated)
      --to int              End time in epoch milliseconds
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

* [gcx annotations](gcx_annotations.md)	 - Manage Grafana annotations

