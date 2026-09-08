## gcx k6 runs query

Query metric values for a k6 test run.

### Synopsis

Query metric values for one k6 test run. The default is a range query over the run duration. Set --aggregate to return one value per series.

```
gcx k6 runs query <run-id> [EXPR] [flags]
```

### Examples

```
  gcx k6 runs query 12345 'histogram_avg' --metric http_req_duration
  gcx k6 runs query 12345 'histogram_quantile(0.95)' --metric 'http_req_duration{scenario="api"}' --aggregate
  gcx k6 runs query 12345 'rate' --metric http_reqs --since 15m --step 30s -o wide
```

### Options

```
      --aggregate       Return one value for the selected run duration
      --expr string     Query expression (alternative to positional argument)
      --from string     Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help            help for query
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --metric string   Metric name with optional label selectors (required)
  -o, --output string   Output format. One of: agents, json, table, wide, yaml (default "table")
      --since string    Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --step string     Query step (e.g., '15s', '1m')
      --to string       End time (RFC3339, Unix timestamp, or relative like 'now')
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

* [gcx k6 runs](gcx_k6_runs.md)	 - Manage k6 test runs.

