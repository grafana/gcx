## gcx fleet collectors get

Get a collector by ID or name.

### Synopsis

Get one Fleet Management collector by ID or name.

Structured output includes local and remote attributes plus the timestamps that
the Fleet API reports. Use table or wide output for a human-readable health view.

```
gcx fleet collectors get <id|name> [flags]
```

### Examples

```
  # Get the full collector resource
  gcx fleet collectors get <id>

  # Show the collector health fields as a table
  gcx fleet collectors get <id> -o wide

  # Select attributes and update time
  gcx fleet collectors get <id> --json spec.local_attributes,spec.remote_attributes,spec.updated_at
```

### Options

```
  -h, --help            help for get
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of dotted field paths to include in JSON output (e.g. spec.name), or 'list' (or '?') to discover the available paths
  -o, --output string   Output format. One of: agents, json, table, wide, yaml (default "yaml")
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx fleet collectors](gcx_fleet_collectors.md)	 - Manage Fleet Management collectors.

