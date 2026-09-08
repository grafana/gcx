## gcx k6 runs wait

Wait for a k6 test run and metric processing to finish.

### Synopsis

Poll a k6 test run until it completes or is aborted. The command waits through the processing_metrics status and exits with an error when the final result did not pass.

```
gcx k6 runs wait <run-id> [flags]
```

### Examples

```
  gcx k6 runs wait 12345
  gcx k6 runs wait 12345 --timeout 30m -o json
```

### Options

```
  -h, --help               help for wait
      --jq string          jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string        Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string      Output format. One of: agents, json, table, yaml (default "table")
      --timeout duration   Maximum time to wait for a terminal run result (default 15m0s)
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

