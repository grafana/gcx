## grafanactl k6 testrun status

Show the most recent test run status for a k6 load test.

```
grafanactl k6 testrun status [test-name] [flags]
```

### Options

```
  -h, --help             help for status
      --id int           Load test ID (skip name lookup)
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

* [grafanactl k6 testrun](grafanactl_k6_testrun.md)	 - Manage k6 TestRun CRD manifests.

