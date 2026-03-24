## grafanactl k6 env-vars create

Create a K6 environment variable from a file.

```
grafanactl k6 env-vars create [flags]
```

### Options

```
  -f, --filename string   File containing the env var JSON (use - for stdin)
  -h, --help              help for create
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

* [grafanactl k6 env-vars](grafanactl_k6_env-vars.md)	 - Manage K6 Cloud environment variables.

