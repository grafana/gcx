## gcx faro apps get

Get a Faro app by slug-id or name.

```
gcx faro apps get [slug-id] [flags]
```

### Examples

```
  # Get by slug-id.
  gcx faro apps get my-web-app-42

  # Get by name.
  gcx faro apps get --name "My Web App"
```

### Options

```
  -h, --help            help for get
      --json string     Comma-separated list of fields to include in JSON output, or '?' to discover available fields
      --name string     Get Faro app by name instead of slug-id
  -o, --output string   Output format. One of: json, table, wide, yaml (default "table")
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

