## gcx k6 test-run runs list

List all test runs for a k6 load test.

### Synopsis

List all test runs for a k6 load test.

Use JSON or YAML to inspect the returned run configuration, including options.thresholds. Threshold expressions are configuration, not evaluated results.

```
gcx k6 test-run runs list [test-name] [flags]
```

### Examples

```
  # Inspect configured run thresholds
  gcx k6 test-run runs list --id 6 --json id,options.thresholds
```

### Options

```
  -h, --help             help for list
      --id int           Load test ID (skip name lookup)
      --jq string        jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string      Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int        Maximum number of items to return (0 for all) (default 50)
  -o, --output string    Output format. One of: agents, json, table, yaml (default "table")
      --project-id int   k6 Cloud project ID (required when using name lookup)
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

* [gcx k6 test-run runs](gcx_k6_test-run_runs.md)	 - Query k6 Cloud test run history.

