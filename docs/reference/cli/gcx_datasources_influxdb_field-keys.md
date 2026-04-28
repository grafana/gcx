## gcx datasources influxdb field-keys

List field keys

### Synopsis

List field keys from an InfluxDB datasource. Only supported in InfluxQL mode.

```
gcx datasources influxdb field-keys [flags]
```

### Examples

```

  # List all field keys (use datasource UID, not name)
  gcx datasources influxdb field-keys -d UID

  # Filter by measurement
  gcx datasources influxdb field-keys -d UID --measurement cpu

  # Output as JSON
  gcx datasources influxdb field-keys -d UID -o json
```

### Options

```
  -d, --datasource string    Datasource UID (required unless datasources.influxdb is configured)
  -h, --help                 help for field-keys
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -m, --measurement string   Filter by measurement name
  -o, --output string        Output format. One of: json, table, yaml (default "table")
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx datasources influxdb](gcx_datasources_influxdb.md)	 - Query InfluxDB datasources

