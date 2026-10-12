## gcx alert rules list

List alert rules.

```
gcx alert rules list [flags]
```

### Options

```
      --folder string   Filter by folder UID
      --group string    Filter by group name
  -h, --help            help for list
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int       Maximum number of items to return (0 for unlimited) (default 50)
  -o, --output string   Output format. One of: agents, json, table, wide, yaml (default "table")
      --state string    Filter by rule state (firing, pending, inactive)
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from known agent identity variables. Set GCX_AGENT_NAME to identify a supported harness, or GCX_AGENT_MODE to control the mode.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx alert rules](gcx_alert_rules.md)	 - Inspect alert rule state and health.

