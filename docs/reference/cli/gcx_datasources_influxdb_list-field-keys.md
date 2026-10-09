## gcx datasources influxdb list-field-keys

List field keys

### Synopsis

List field keys from an InfluxDB datasource. Only supported in InfluxQL mode.

```
gcx datasources influxdb list-field-keys [flags]
```

### Examples

```

  # List all field keys (use datasource UID, not name)
  gcx datasources influxdb list-field-keys -d UID

  # Filter by measurement
  gcx datasources influxdb list-field-keys -d UID --measurement cpu

  # Output as JSON
  gcx datasources influxdb list-field-keys -d UID -o json
```

### Options

```
  -d, --datasource string    Datasource UID (required unless datasources.influxdb is configured)
  -h, --help                 help for list-field-keys
      --jq string            jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -m, --measurement string   Filter by measurement name
  -o, --output string        Output format. One of: agents, json, table, yaml (default "table")
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

* [gcx datasources influxdb](gcx_datasources_influxdb.md)	 - Query InfluxDB datasources

