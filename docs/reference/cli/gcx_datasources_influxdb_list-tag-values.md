## gcx datasources influxdb list-tag-values

List tag values

### Synopsis

List tag values for a given key from an InfluxDB datasource. Only supported in InfluxQL mode.

```
gcx datasources influxdb list-tag-values [flags]
```

### Examples

```

  # List values for a tag key (use datasource UID, not name)
  gcx datasources influxdb list-tag-values -d UID --key host

  # Filter by measurement
  gcx datasources influxdb list-tag-values -d UID --key host --measurement cpu

  # Output as JSON
  gcx datasources influxdb list-tag-values -d UID --key host -o json
```

### Options

```
  -d, --datasource string    Datasource UID (required unless datasources.influxdb is configured)
  -h, --help                 help for list-tag-values
      --jq string            jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -k, --key string           Tag key to get values for (required)
  -m, --measurement string   Filter by measurement name
  -o, --output string        Output format. One of: agents, json, table, yaml (default "table")
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

* [gcx datasources influxdb](gcx_datasources_influxdb.md)	 - Query InfluxDB datasources

