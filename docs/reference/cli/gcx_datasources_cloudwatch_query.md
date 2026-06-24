## gcx datasources cloudwatch query

Execute a CloudWatch metric query

### Synopsis

Execute a CloudWatch metric query.

Queries are structured (namespace, metric, region, statistic, period, dimensions) —
there is no expression language for CloudWatch. Use --dimensions (repeatable) for
dimension filters, or omit them to aggregate across all combinations.

Use --share-link to print the equivalent Grafana Explore URL after the query.
Note: when no --from/--to/--since flags are provided, the share link encodes
"now-1h"/"now" (relative), not the absolute window the CLI just queried.

Cross-account monitoring datasources: if your datasource is configured for
cross-account monitoring (a "monitoring account"), --dimensions filters scope
to the datasource's own account by default. To surface dimensions from source
accounts, pass --account-id <id> (run list-accounts to discover IDs) or
--account-id all to query all linked accounts.

```
gcx datasources cloudwatch query [flags]
```

### Examples

```

  # Query with required flags
  gcx datasources cloudwatch query -d UID --region us-east-1 --namespace AWS/EC2 --metric CPUUtilization

  # With time range
  gcx datasources cloudwatch query -d UID --region us-east-1 --namespace AWS/EC2 --metric CPUUtilization --since 1h

  # With dimension filter
  gcx datasources cloudwatch query -d UID --region us-east-1 --namespace AWS/EC2 --metric CPUUtilization \
    --dimensions InstanceId=i-0123456789abcdef0 --since 1h

  # Print as JSON
  gcx datasources cloudwatch query -d UID --region us-east-1 --namespace AWS/EC2 --metric CPUUtilization -o json
```

### Options

```
      --account-id string           AWS account ID for cross-account monitoring; pass 'all' to query all linked accounts, or a specific ID from list-accounts. Required to surface dimensions from source accounts on monitoring datasources.
  -d, --datasource string           Datasource UID (required unless datasources.cloudwatch is configured)
      --dimensions stringToString   Dimension key=value pairs (repeatable, e.g. --dimensions InstanceId=i-abc) (default [])
      --from string                 Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                        help for query
      --jq string                   jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string                 Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --metric string               CloudWatch metric name, e.g. CPUUtilization (required)
      --namespace string            CloudWatch namespace, e.g. AWS/EC2 (required)
      --open                        Open the executed query in Grafana Explore
  -o, --output string               Output format. One of: agents, graph, json, table, wide, yaml (default "table")
      --period string               Period in seconds (e.g. 60, 300) or "auto" to let CloudWatch pick a period that fits the time range (default "auto")
      --region string               AWS region, e.g. us-east-1 (required)
      --share-link                  Print the Grafana Explore URL for the executed query to stderr
      --since string                Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --statistic string            Statistic: Average, Sum, Maximum, Minimum, SampleCount, or a percentile/trimmed-mean (e.g. p95, p99, tm99) (default "Average")
      --to string                   End time (RFC3339, Unix timestamp, or relative like 'now')
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

* [gcx datasources cloudwatch](gcx_datasources_cloudwatch.md)	 - Query AWS CloudWatch datasources

