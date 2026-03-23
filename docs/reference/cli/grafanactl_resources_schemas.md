## grafanactl resources schemas

List available Grafana API resource types

### Synopsis

List available Grafana API resource types and their schemas. Optionally filter by a resource selector.

```
grafanactl resources schemas [RESOURCE_SELECTOR] [flags]
```

### Examples

```

	grafanactl resources schemas
	grafanactl resources schemas -o wide
	grafanactl resources schemas -o json
	grafanactl resources schemas -o yaml
	grafanactl resources schemas -o json --no-schema
	grafanactl resources schemas incidents
	grafanactl resources schemas incidents.v1alpha1.incident.ext.grafana.app -o json

```

### Options

```
  -h, --help            help for schemas
      --json string     Comma-separated list of fields to include in JSON output, or '?' to discover available fields
      --no-schema       Skip fetching OpenAPI spec schemas (faster, omits schema info and unlistable resource types)
  -o, --output string   Output format. One of: json, text, wide, yaml (default "text")
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GRAFANACTL_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [grafanactl resources](grafanactl_resources.md)	 - Manipulate Grafana resources

