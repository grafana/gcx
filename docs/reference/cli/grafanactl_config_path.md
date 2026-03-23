## grafanactl config path

Show loaded config file paths

### Synopsis

Display all config files that contribute to the merged configuration, ordered by priority.

```
grafanactl config path [flags]
```

### Options

```
  -h, --help            help for path
  -o, --output string   Output format. One of: json, table, yaml (default "table")
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

* [grafanactl config](grafanactl_config.md)	 - View or manipulate configuration settings

