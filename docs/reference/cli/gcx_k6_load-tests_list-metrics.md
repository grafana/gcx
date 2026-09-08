## gcx k6 load-tests list-metrics

List metric metadata across k6 test runs.

### Synopsis

List metric metadata across selected runs of one k6 load test. Selectors are mutually exclusive. If neither selector is set, the server uses the last 30 runs.

```
gcx k6 load-tests list-metrics <load-test-id> [flags]
```

### Examples

```
  gcx k6 load-tests list-metrics 12345
  gcx k6 load-tests list-metrics 12345 --run-count 5
  gcx k6 load-tests list-metrics 12345 --run-id 1001,1002 -o json
```

### Options

```
  -h, --help            help for list-metrics
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, table, yaml (default "table")
      --run-count int   Use the last N test runs (default: the server uses the last 30 runs)
      --run-id ints     Use specific test run IDs (repeatable or comma-separated)
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

