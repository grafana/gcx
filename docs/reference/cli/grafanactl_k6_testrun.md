## grafanactl k6 testrun

Manage k6 TestRun CRD manifests.

### Options

```
  -h, --help   help for testrun
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

* [grafanactl k6](grafanactl_k6.md)	 - Manage K6 Cloud resources (projects, tests, env vars, schedules, load zones).
* [grafanactl k6 testrun emit](grafanactl_k6_testrun_emit.md)	 - Fetch a k6 Cloud test and emit Kubernetes TestRun CRD manifests.
* [grafanactl k6 testrun runs](grafanactl_k6_testrun_runs.md)	 - Query k6 Cloud test run history.
* [grafanactl k6 testrun status](grafanactl_k6_testrun_status.md)	 - Show the most recent test run status for a k6 load test.

