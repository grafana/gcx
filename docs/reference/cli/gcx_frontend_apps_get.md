## gcx frontend apps get

Get a Frontend Observability app by slug-id or name.

### Synopsis

Get a Frontend Observability app by slug-id (my-web-app-42), numeric ID or
display name. An argument that matches no app by ID is looked up by name.

```
gcx frontend apps get <slug-id|name> [flags]
```

### Examples

```
  # Get by slug-id.
  gcx frontend apps get my-web-app-42

  # Get by name.
  gcx frontend apps get "My Web App"
```

### Options

```
  -h, --help            help for get
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, table, wide, yaml (default "table")
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

* [gcx frontend apps](gcx_frontend_apps.md)	 - Manage Frontend Observability apps.

