## gcx resources list-types

List available Grafana API resource types

### Synopsis

List available Grafana API resource types and their schemas by querying a live Grafana instance. Requires a connection to Grafana. Use --no-schema to skip OpenAPI spec fetching for faster results. Optionally filter by a resource selector.

```
gcx resources list-types [RESOURCE_SELECTOR] [flags]
```

### Examples

```

	gcx resources list-types
	gcx resources list-types -o wide
	gcx resources list-types -o json
	gcx resources list-types -o yaml
	gcx resources list-types -o json --no-schema
	gcx resources list-types incidents
	gcx resources list-types incidents.v1alpha1.incident.ext.grafana.app -o json

```

### Options

```
  -h, --help            help for list-types
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --no-schema       Skip fetching OpenAPI spec schemas (faster, omits schema info and unlistable resource types)
  -o, --output string   Output format. One of: agents, json, text, wide, yaml (default "text")
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx resources](gcx_resources.md)	 - Manipulate Grafana resources

