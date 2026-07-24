## gcx datasources pyroscope labels

List labels, label values, or label sets

### Synopsis

List all labels, get values for one label, or list unique label sets from a
Pyroscope datasource.

EXPR is an optional label selector (e.g., '{service_name="frontend"}') that
scopes the results to matching series. Selector-scoped requests and multi-label
requests are answered from the Series API, so they reflect exactly the series
matching the selector.

```
gcx datasources pyroscope labels [EXPR] [flags]
```

### Examples

```

	# List all labels (use datasource UID, not name)
	gcx datasources pyroscope labels -d UID

	# Get values for a specific label
	gcx datasources pyroscope labels -d UID --label service_name

	# Labels present on series matching a selector
	gcx datasources pyroscope labels -d UID '{service_name="frontend"}'

	# Values of a label, scoped to a selector
	gcx datasources pyroscope labels -d UID '{namespace="prod"}' -l service_name

	# Unique label sets (one column per label)
	gcx datasources pyroscope labels -d UID -l service_name -l namespace

	# Search a wider window than the default last hour
	gcx datasources pyroscope labels -d UID --since 24h

	# Output as JSON
	gcx datasources pyroscope labels -d UID -o json
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.pyroscope is configured)
      --expr string         Label selector to scope the results (alternative to positional argument)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for labels
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -l, --label strings       Get values for this label; repeat to list unique label sets (omit to list all labels)
  -o, --output string       Output format. One of: agents, json, table, yaml (default "table")
      --since string        Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
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

* [gcx datasources pyroscope](gcx_datasources_pyroscope.md)	 - Query Pyroscope datasources

