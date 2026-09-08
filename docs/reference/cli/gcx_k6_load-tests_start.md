## gcx k6 load-tests start

Start a saved k6 Cloud load test.

### Synopsis

Start a saved k6 Cloud load test. This operation can consume billable VUh.

```
gcx k6 load-tests start <load-test-id> [flags]
```

### Options

```
  -h, --help                     help for start
      --idempotency-key string   Idempotency key, 1 to 36 characters
      --jq string                jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string              Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string            Output format. One of: agents, json, table, yaml (default "table")
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

