## grafanactl k6

Manage K6 Cloud resources (projects, tests, env vars).

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for k6
```

### Options inherited from parent commands

```
      --agent           Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GRAFANACTL_AGENT_MODE env vars.
      --no-color        Disable color output
      --no-truncate     Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count   Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [grafanactl](grafanactl.md)	 - 
* [grafanactl k6 envvars](grafanactl_k6_envvars.md)	 - Manage K6 Cloud environment variables.
* [grafanactl k6 projects](grafanactl_k6_projects.md)	 - Manage K6 Cloud projects.
* [grafanactl k6 runs](grafanactl_k6_runs.md)	 - Manage K6 test runs.
* [grafanactl k6 tests](grafanactl_k6_tests.md)	 - Manage K6 Cloud load tests.
* [grafanactl k6 token](grafanactl_k6_token.md)	 - Print the authenticated k6 API token.

