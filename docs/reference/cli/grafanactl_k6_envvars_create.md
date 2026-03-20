## grafanactl k6 envvars create

Create a K6 environment variable from a file.

```
grafanactl k6 envvars create [flags]
```

### Options

```
  -f, --filename string   File containing the env var JSON (use - for stdin)
  -h, --help              help for create
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

* [grafanactl k6 envvars](grafanactl_k6_envvars.md)	 - Manage K6 Cloud environment variables.

