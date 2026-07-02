## gcx datasources schemas get

Get the configuration schema for a datasource plugin type

### Synopsis

Get the configuration schema for a datasource plugin type.

The schema lists the configuration fields available for a plugin, derived from
the server's OpenAPI document. Use it to author create/update manifests.

```
gcx datasources schemas get [flags]
```

### Examples

```

	# Show a plugin's configuration schema
	gcx datasources schemas get --type grafana-sentry-datasource

	# As JSON
	gcx datasources schemas get --type grafana-sentry-datasource -o json
```

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for get
      --jq string        jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string      Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --kind string      Schema kind: config (query is not yet supported) (default "config")
  -o, --output string    Output format. One of: agents, json, yaml (default "yaml")
  -t, --type string      Datasource plugin id (e.g. grafana-sentry-datasource)
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

* [gcx datasources schemas](gcx_datasources_schemas.md)	 - Inspect datasource plugin schemas

