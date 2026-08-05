## gcx datasources cloudmonitoring list-metrics

List metric descriptors for a GCP project

### Synopsis

List the Cloud Monitoring metric descriptors available in a project, with
kind, value type, and unit. Use --service to narrow the listing (recommended;
unfiltered listings page through every metric in the project and can be slow).

```
gcx datasources cloudmonitoring list-metrics [flags]
```

### Examples

```

  gcx datasources cloudmonitoring list-metrics -d UID --project my-project --service compute.googleapis.com
  gcx datasources cloudmonitoring list-metrics -d UID --project my-project --service monitoring.googleapis.com -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.cloudmonitoring is configured)
  -h, --help                help for list-metrics
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, json, table, yaml (default "table")
      --project string      GCP project ID (required)
      --service string      Restrict to metrics of this service prefix, e.g. compute.googleapis.com (recommended: unfiltered listings page through every metric and can be slow)
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

* [gcx datasources cloudmonitoring](gcx_datasources_cloudmonitoring.md)	 - Query Google Cloud Monitoring datasources

