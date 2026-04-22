## gcx k6 test-run

Manage k6 TestRun CRD manifests.

### Options

```
  -h, --help   help for test-run
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --limit int          Maximum number of items to return from list operations (0 for all; defaults to 50 in agent mode)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx k6](gcx_k6.md)	 - Manage Grafana k6 Cloud projects, load tests, and schedules
* [gcx k6 test-run emit](gcx_k6_test-run_emit.md)	 - Fetch a k6 Cloud test and emit Kubernetes TestRun CRD manifests.
* [gcx k6 test-run runs](gcx_k6_test-run_runs.md)	 - Query k6 Cloud test run history.
* [gcx k6 test-run status](gcx_k6_test-run_status.md)	 - Show the most recent test run status for a k6 load test.

