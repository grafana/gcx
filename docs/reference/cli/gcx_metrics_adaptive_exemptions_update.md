## gcx metrics adaptive exemptions update

Update a recommendation exemption.

```
gcx metrics adaptive exemptions update <id> [flags]
```

### Options

```
      --active-interval string    Active interval (e.g. 30d, 1h)
      --disable-recommendations   Disable all recommendations for matched metrics
  -h, --help                      help for update
      --jq string                 jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string               Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --keep-labels strings       Labels to keep (comma-separated)
      --managed-by string         Manager identifier
      --match-type string         Match type: exact, prefix, or suffix
      --metric string             Metric name or pattern
  -o, --output string             Output format. One of: agents, json, yaml (default "json")
      --reason string             Reason for the exemption
      --segment string            Segment ID
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

* [gcx metrics adaptive exemptions](gcx_metrics_adaptive_exemptions.md)	 - Manage Adaptive Metrics recommendation exemptions.

