## gcx datasources health

Check the health of one or more datasources

### Synopsis

Check datasource health via the Grafana datasource health endpoint.

With a UID, checks a single datasource. Without arguments, checks all
datasources. Use --type to check all datasources of a given plugin type.

Exit codes distinguish resource failure from command failure:
  0 - all checked datasources are healthy
  4 - the check ran but one or more datasources are unhealthy (resource failure)
  1/2/3 - the check could not run (operational, usage, or auth failure)

```
gcx datasources health [UID] [flags]
```

### Examples

```

	# Check a single datasource
	gcx datasources health my-ds-uid

	# Check all datasources
	gcx datasources health

	# Check all datasources of a given type
	gcx datasources health --type grafana-sentry-datasource
```

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for health
      --jq string        jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string      Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string    Output format. One of: agents, json, table, yaml (default "table")
  -t, --type string      Filter by datasource type (e.g., prometheus, grafana-sentry-datasource)
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

