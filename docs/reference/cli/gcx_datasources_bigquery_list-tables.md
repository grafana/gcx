## gcx datasources bigquery list-tables

List tables in a BigQuery dataset

### Synopsis

List tables in a BigQuery dataset via INFORMATION_SCHEMA.TABLES.

--dataset is required. When --project is omitted, the datasource's default
project is used. Run 'list-datasets' to discover available datasets.

At most 1000 tables are returned; additional tables are not listed.

```
gcx datasources bigquery list-tables [flags]
```

### Examples

```

  # List tables in a dataset (default project)
  gcx datasources bigquery list-tables --dataset my_dataset

  # List tables in a dataset in a specific project
  gcx datasources bigquery list-tables --project my-project --dataset my_dataset

  # Output as JSON
  gcx datasources bigquery list-tables --dataset my_dataset -o json
```

### Options

```
      --dataset string      Dataset to list tables from (required)
  -d, --datasource string   Datasource UID (required unless datasources.bigquery is configured)
  -h, --help                help for list-tables
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, json, table, wide, yaml (default "table")
      --project string      GCP project ID (default: the datasource's default project)
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

* [gcx datasources bigquery](gcx_datasources_bigquery.md)	 - Query BigQuery datasources

