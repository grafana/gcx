## gcx datasources bigquery describe-table

Show column schema for a BigQuery table

### Synopsis

Show column name, data type, and nullability for each column in a table via
INFORMATION_SCHEMA.COLUMNS.

The dataset is required, supplied either in the table name (DATASET.TABLE or
PROJECT.DATASET.TABLE) or via --dataset. When the project is omitted, the
datasource's default project is used.

```
gcx datasources bigquery describe-table TABLE [flags]
```

### Examples

```

  # Describe a table in a dataset (default project; equivalent forms)
  gcx datasources bigquery describe-table my_dataset.events
  gcx datasources bigquery describe-table events --dataset my_dataset

  # Describe a table in a specific project (equivalent forms)
  gcx datasources bigquery describe-table my-project.my_dataset.events
  gcx datasources bigquery describe-table events --project my-project --dataset my_dataset

  # Output as JSON
  gcx datasources bigquery describe-table my_dataset.events -o json
```

### Options

```
      --dataset string      Dataset containing the table (required)
  -d, --datasource string   Datasource UID (required unless datasources.bigquery is configured)
  -h, --help                help for describe-table
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

