## gcx datasources pyroscope series

List unique profile label sets

### Synopsis

List unique profile label sets from a Pyroscope datasource.

The command uses Pyroscope's Series endpoint and does not require a profile
type. SELECTOR is optional; use --match for repeatable selectors. Multiple
selectors are combined as a union. By default, the response includes every
label.

Use --label-name to request only the labels needed for discovery. This reduces
the response size and can significantly speed up queries with high-cardinality
labels. For example, use --label-name service_name --label-name namespace to
discover services and namespaces without fetching pod, instance, or custom
labels.

```
gcx datasources pyroscope series [SELECTOR] [flags]
```

### Examples

```

	# List service and workload combinations from the last hour
	gcx datasources pyroscope series -d UID --since 1h

	# Faster discovery: request only the labels needed
	gcx datasources pyroscope series -d UID '{service_name="checkout"}' \
		--label-name service_name --label-name namespace --label-name pod --since 7d

	# Use multiple selectors and JSON output
	gcx datasources pyroscope series -d UID \
		--match '{namespace="payments"}' --match '{namespace="checkout"}' \
		--since 24h -o json
```

### Options

```
  -d, --datasource string    Datasource UID (required unless datasources.pyroscope is configured)
      --from string          Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                 help for series
      --jq string            jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --label-name strings   Label name to return (repeatable; limit labels to reduce response size and speed up discovery)
      --match stringArray    Profile label selector (repeatable; selectors are combined as a union)
  -o, --output string        Output format. One of: agents, json, table, wide, yaml (default "table")
      --since string         Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --to string            End time (RFC3339, Unix timestamp, or relative like 'now')
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx datasources pyroscope](gcx_datasources_pyroscope.md)	 - Query Pyroscope datasources

