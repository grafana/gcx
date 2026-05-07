## gcx datasources synthetic-monitoring checks

List Synthetic Monitoring checks

### Synopsis

List all checks accessible through the configured Synthetic Monitoring datasource.

```
gcx datasources synthetic-monitoring checks [flags]
```

### Examples

```

  # List checks (use datasource UID, not name)
  gcx datasources synthetic-monitoring checks -d UID

  # List checks with their alert rules embedded (one server-side call)
  gcx datasources synthetic-monitoring checks -d UID --with-alerts

  # Filter by job/target substring
  gcx datasources synthetic-monitoring checks -d UID --search api-prod

  # Only disabled checks
  gcx datasources synthetic-monitoring checks -d UID --enabled=false

  # Aggressive checks (faster than 1 minute)
  gcx datasources synthetic-monitoring checks -d UID --max-frequency 1m

  # Combine filters
  gcx datasources synthetic-monitoring checks -d UID --search staging --enabled --max-frequency 30s

  # Output as JSON
  gcx datasources synthetic-monitoring checks -d UID -o json
```

### Options

```
  -d, --datasource string        Datasource UID (required unless datasources.synthetic-monitoring is configured)
      --enabled                  Restrict to enabled (--enabled=true) or disabled (--enabled=false) checks; omit for no filter
  -h, --help                     help for checks
      --json string              Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --max-frequency duration   Upper bound on check frequency, inclusive (e.g. 30s, 5m). Sent to the API as max_frequency in milliseconds.
      --min-frequency duration   Lower bound on check frequency, inclusive (e.g. 30s, 5m). Sent to the API as min_frequency in milliseconds.
  -o, --output string            Output format. One of: json, yaml (default "json")
      --search string            Case-insensitive substring match against the check's job and target
      --with-alerts              Include each check's alert rules in the response (server-side composition via ?includeAlerts=true). Cannot be combined with --search/--enabled/--min-frequency/--max-frequency.
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use (overrides current-context in config)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx datasources synthetic-monitoring](gcx_datasources_synthetic-monitoring.md)	 - Query Synthetic Monitoring datasources

