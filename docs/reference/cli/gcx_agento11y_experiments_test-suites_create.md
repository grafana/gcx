## gcx agento11y experiments test-suites create

Create a test suite.

```
gcx agento11y experiments test-suites create [flags]
```

### Options

```
      --description string   Test suite description
  -f, --filename string      File containing the test suite create payload (use - for stdin)
  -h, --help                 help for create
      --jq string            jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --name string          Test suite name
  -o, --output string        Output format. One of: agents, json, yaml (default "json")
      --suite-id string      Stable test suite id
      --tag strings          Test suite tag (repeatable or comma-separated)
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

* [gcx agento11y experiments test-suites](gcx_agento11y_experiments_test-suites.md)	 - Manage experiment test suites.

