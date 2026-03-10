## grafanactl slo definitions pull

Pull SLO definitions to disk.

```
grafanactl slo definitions pull [flags]
```

### Options

```
  -h, --help                help for pull
  -d, --output-dir string   Directory to write SLO definition files to (default ".")
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

* [grafanactl slo definitions](grafanactl_slo_definitions.md)	 - Manage SLO definitions.

