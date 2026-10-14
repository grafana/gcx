## gcx datasources newrelic query

Run NRQL through an open-source New Relic plugin datasource

### Synopsis

Query the open-source New Relic Grafana datasource plugin with NRQL.
The datasource is selected by -d UID, contexts.<name>.datasources.newrelic,
or discovery when exactly one matching plugin instance is installed.
The plugin uses the selected Grafana time range
unless NRQL has SINCE or COMPARE WITH.
With no time flags, the range defaults to the last hour.
Use --share-link to print an Explore URL.
Use --open to open it after the query succeeds.

```
gcx datasources newrelic query [EXPR] [flags]
```

### Examples

```
  gcx datasources newrelic query -d UID 'SELECT count(*) FROM Transaction' --since 1h
  gcx datasources newrelic query -d UID 'SELECT * FROM Transaction' --account-id 12345 --timeout-seconds 30 -o json
```

### Options

```
      --account-id int        New Relic account ID override (positive integer; omit to use plugin configuration)
  -d, --datasource string     New Relic plugin datasource UID (auto-discovers one if omitted)
      --expr string           Query expression (alternative to positional argument)
      --from string           Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                  help for query
      --jq string             jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string           Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --open                  Open the executed query in Grafana Explore
  -o, --output string         Output format. One of: agents, json, table, wide, yaml (default "table")
      --share-link            Print the Grafana Explore URL for the executed query to stderr
      --since string          Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --timeout-seconds int   Query timeout override (5-120 seconds; omit to use plugin default)
      --to string             End time (RFC3339, Unix timestamp, or relative like 'now')
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx datasources newrelic](gcx_datasources_newrelic.md)	 - Query New Relic plugin datasources

