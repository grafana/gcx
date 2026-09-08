## gcx k6 load-tests query

Query aggregate metric values across k6 test runs.

### Synopsis

Run one aggregate metric query across selected runs of a k6 load test. Select the last N runs or explicit run IDs.

```
gcx k6 load-tests query <load-test-id> [EXPR] [flags]
```

### Examples

```
  gcx k6 load-tests query 12345 'histogram_quantile(0.95)' --metric http_req_duration --run-count 5
  gcx k6 load-tests query 12345 'sum' --metric http_reqs --run-id 1001,1002 -o json
```

### Options

```
      --expr string     Query expression (alternative to positional argument)
  -h, --help            help for query
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --metric string   Metric name with optional label selectors (required)
  -o, --output string   Output format. One of: agents, json, table, wide, yaml (default "table")
      --run-count int   Query the last N test runs
      --run-id ints     Query specific test run IDs (repeatable or comma-separated)
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

* [gcx k6 load-tests](gcx_k6_load-tests.md)	 - Manage k6 Cloud load tests.

