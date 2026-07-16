## gcx profiles list-profile-types

List available profile types

### Synopsis

List available profile types from a Pyroscope datasource.

If gcx auto-discovers the datasource from your Grafana Cloud stack, the discovered datasource UID may be saved to your gcx configuration for future commands.

```
gcx profiles list-profile-types [flags]
```

### Examples

```

  # List profile types (use datasource UID, not name)
  gcx profiles list-profile-types -d UID

  # Output as JSON
  gcx profiles list-profile-types -d UID -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless default-pyroscope-datasource is configured)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for list-profile-types
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, json, table, yaml (default "table")
      --since string        Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx profiles](gcx_profiles.md)	 - Query Pyroscope datasources and manage continuous profiling

