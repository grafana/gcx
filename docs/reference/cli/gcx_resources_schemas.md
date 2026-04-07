## gcx resources schemas

List available Grafana API resource types

### Synopsis

List available Grafana API resource types and their schemas by querying a live Grafana instance. Requires a connection to Grafana. Use --no-schema to skip OpenAPI spec fetching for faster results. Optionally filter by a resource selector.

```
gcx resources schemas [RESOURCE_SELECTOR] [flags]
```

### Examples

```

	gcx resources schemas
	gcx resources schemas -o wide
	gcx resources schemas -o json
	gcx resources schemas -o yaml
	gcx resources schemas -o json --no-schema
	gcx resources schemas incidents
	gcx resources schemas incidents.v1alpha1.incident.ext.grafana.app -o json

```

### Options

```
  -h, --help            help for schemas
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --no-schema       Skip fetching OpenAPI spec schemas (faster, omits schema info and unlistable resource types)
  -o, --output string   Output format. One of: json, text, wide, yaml (default "text")
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx resources](gcx_resources.md)	 - Manipulate Grafana resources

