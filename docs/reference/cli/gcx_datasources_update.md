## gcx datasources update

Update a datasource from a manifest file

### Synopsis

Update an existing datasource instance from a declarative manifest file.

This is a full replace: fields omitted from the manifest are reset. The current
resourceVersion is fetched and applied automatically (optimistic concurrency).
Secret values are updated via the top-level secure block. update does not
prompt; use --dry-run to preview the change.

```
gcx datasources update UID [flags]
```

### Examples

```

	# Update a datasource from a YAML manifest
	gcx datasources update my-ds-uid -f sentry.yaml

	# Preview the change
	gcx datasources update my-ds-uid -f sentry.yaml --dry-run
```

### Options

```
      --config string         Path to the configuration file to use
      --context string        Name of the context to use
      --dry-run               Preview the change without applying it
  -f, --filename string       File containing the datasource manifest (use - for stdin)
  -h, --help                  help for update
      --jq string             jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string           Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string         Output format. One of: agents, json, yaml (default "yaml")
      --secrets-file string   File containing secret values to merge into the secure block
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

