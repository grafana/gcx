## grafanactl k6 tests

Manage K6 Cloud load tests.

### Options

```
  -h, --help   help for tests
```

### Options inherited from parent commands

```
      --agent               Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GRAFANACTL_AGENT_MODE env vars.
      --api-domain string   K6 Cloud API domain (default: https://api.k6.io)
      --config string       Path to the configuration file to use
      --context string      Name of the context to use
      --no-color            Disable color output
      --no-truncate         Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count       Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [grafanactl k6](grafanactl_k6.md)	 - Manage K6 Cloud resources (projects, tests, env vars).
* [grafanactl k6 tests delete](grafanactl_k6_tests_delete.md)	 - Delete a K6 load test.
* [grafanactl k6 tests get](grafanactl_k6_tests_get.md)	 - Get a single K6 load test by ID.
* [grafanactl k6 tests list](grafanactl_k6_tests_list.md)	 - List K6 Cloud load tests.

