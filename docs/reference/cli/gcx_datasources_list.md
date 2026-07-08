## gcx datasources list

List all datasources

### Synopsis

List all datasources configured in Grafana. Filter by type and/or name (case-insensitive substring match).

```
gcx datasources list [flags]
```

### Examples

```

	# List all datasources
	gcx datasources list

	# List only Prometheus datasources
	gcx datasources list --type prometheus

	# Filter by name substring (matches "prometheus-prod-eu", "loki-prod-us", ...)
	gcx datasources list --name prod

	# Combine type and name filters
	gcx datasources list --type prometheus --name prod

	# Output as JSON
	gcx datasources list -o json
```

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for list
      --jq string        jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string      Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int        Maximum number of datasources to return (default 50)
      --name string      Filter by datasource name (case-insensitive substring match)
  -o, --output string    Output format. One of: agents, json, table, yaml (default "table")
  -t, --type string      Filter by datasource type (e.g., prometheus, loki)
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx datasources](gcx_datasources.md)	 - Manage and query Grafana datasources

