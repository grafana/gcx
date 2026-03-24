## grafanactl k6 testrun runs list

List all test runs for a k6 load test.

```
grafanactl k6 testrun runs list [test-name] [flags]
```

### Options

```
  -h, --help             help for list
      --id int           Load test ID (skip name lookup)
      --json string      Comma-separated list of fields to include in JSON output, or '?' to discover available fields
  -o, --output string    Output format. One of: json, table, yaml (default "table")
      --project-id int   k6 Cloud project ID (required when using name lookup)
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GRAFANACTL_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [grafanactl k6 testrun runs](grafanactl_k6_testrun_runs.md)	 - Query k6 Cloud test run history.

