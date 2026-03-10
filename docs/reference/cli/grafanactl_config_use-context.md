## grafanactl config use-context

Set the current context

### Synopsis

Set the current context and updates the configuration file.

```
grafanactl config use-context CONTEXT_NAME [flags]
```

### Examples

```

	grafanactl config use-context dev-instance
```

### Options

```
  -h, --help   help for use-context
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

