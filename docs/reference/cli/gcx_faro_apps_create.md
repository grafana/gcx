## gcx faro apps create

Create a Faro app from a file.

```
gcx faro apps create [flags]
```

### Examples

```
  # Create a Faro app from a YAML file.
  gcx faro apps create -f app.yaml

  # Create from stdin.
  cat app.yaml | gcx faro apps create -f -
```

### Options

```
  -f, --filename string   File containing the Faro app manifest (use - for stdin)
  -h, --help              help for create
      --json string       Comma-separated list of fields to include in JSON output, or '?' to discover available fields
  -o, --output string     Output format. One of: json, yaml (default "yaml")
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

* [gcx faro apps](gcx_faro_apps.md)	 - Manage Faro apps.

