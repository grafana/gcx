## gcx config view

Display the current configuration

```
gcx config view [flags]
```

### Examples

```

	gcx config view
```

### Options

```
  -h, --help            help for view
      --json string     Comma-separated list of fields to include in JSON output, or '?' to discover available fields
      --minify          Remove all information not used by current-context from the output
  -o, --output string   Output format. One of: json, yaml (default "yaml")
      --raw             Display sensitive information
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx config](gcx_config.md)	 - View or manipulate configuration settings

