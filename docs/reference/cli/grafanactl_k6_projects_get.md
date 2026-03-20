## grafanactl k6 projects get

Get a single K6 project by ID.

```
grafanactl k6 projects get <id> [flags]
```

### Options

```
  -h, --help            help for get
      --json string     Comma-separated list of fields to include in JSON output, or '?' to discover available fields
  -o, --output string   Output format. One of: json, yaml (default "yaml")
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

* [grafanactl k6 projects](grafanactl_k6_projects.md)	 - Manage K6 Cloud projects.

