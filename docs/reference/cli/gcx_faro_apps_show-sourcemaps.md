## gcx faro apps show-sourcemaps

Show sourcemaps for a Faro app.

```
gcx faro apps show-sourcemaps <app-name> [flags]
```

### Examples

```
  # List all sourcemaps for an app.
  gcx faro apps show-sourcemaps my-web-app-42

  # List the first 10 sourcemaps.
  gcx faro apps show-sourcemaps my-web-app-42 --limit 10

  # Output as JSON.
  gcx faro apps show-sourcemaps my-web-app-42 -o json
```

### Options

```
  -h, --help            help for show-sourcemaps
      --json string     Comma-separated list of fields to include in JSON output, or '?' to discover available fields
      --limit int       Maximum number of sourcemaps to return (0 for all)
  -o, --output string   Output format. One of: json, text, yaml (default "text")
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

