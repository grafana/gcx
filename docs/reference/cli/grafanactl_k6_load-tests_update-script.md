## grafanactl k6 load-tests update-script

Update the script of a K6 load test from a file.

```
grafanactl k6 load-tests update-script <id> [flags]
```

### Options

```
  -f, --filename string   k6 script file to upload
  -h, --help              help for update-script
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

* [grafanactl k6 load-tests](grafanactl_k6_load-tests.md)	 - Manage K6 Cloud load tests.

