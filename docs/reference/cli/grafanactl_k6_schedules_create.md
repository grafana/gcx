## grafanactl k6 schedules create

Create a K6 schedule from a file.

```
grafanactl k6 schedules create [flags]
```

### Options

```
  -f, --filename string    File containing the schedule request (JSON/YAML)
  -h, --help               help for create
      --load-test-id int   Load test ID (required)
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

* [grafanactl k6 schedules](grafanactl_k6_schedules.md)	 - Manage K6 Cloud schedules.

