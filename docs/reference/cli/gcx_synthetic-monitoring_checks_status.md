## gcx synthetic-monitoring checks status

Show pass/fail status of Synthetic Monitoring checks.

### Synopsis

Show pass/fail status by combining the SM API with the Synthetic Monitoring app's named queries.

Displays reachability (the value on the app's check list card, over the last
3 hours), average latency, number of probes reporting, and health status for
each check. The values come from the Synthetic Monitoring datasource, so no
Prometheus datasource is needed; the stack must run a Synthetic Monitoring app
version that serves the checks_reachability, checks_probe_count and
checks_latency queries. Unlike 'checks timeline', which still queries a
Prometheus datasource, the values here come only from the app.

```
gcx synthetic-monitoring checks status [ID] [flags]
```

### Examples

```
  # Show status of all checks.
  gcx synthetic-monitoring checks status

  # Show status of a specific check by ID.
  gcx synthetic-monitoring checks status 42

  # Filter by job name glob.
  gcx synthetic-monitoring checks status --job 'shopk8s-*'

  # Filter by label and status.
  gcx synthetic-monitoring checks status --label env=prod --status FAILING

  # Output as JSON for scripting.
  gcx synthetic-monitoring checks status -o json
```

### Options

```
  -h, --help                help for status
      --job string          Filter by job name glob pattern (e.g. --job 'shopk8s-*')
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --label stringArray   Filter by label key=value (repeatable, e.g. --label env=prod)
  -o, --output string       Output format. One of: agents, graph, json, table, wide, yaml (default "table")
      --status string       Filter results by status: OK, FAILING, or NODATA
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from known agent identity variables. Set GCX_AGENT_NAME to identify a supported harness, or GCX_AGENT_MODE to control the mode.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx synthetic-monitoring checks](gcx_synthetic-monitoring_checks.md)	 - Manage Synthetic Monitoring checks.

