## gcx resources validate

Validate local resources against a Grafana instance

### Synopsis

Validate local resource files against a remote Grafana instance. Requires a live connection to Grafana for server-side validation. Reads resources from disk and reports validation errors per resource.

```
gcx resources validate [RESOURCE_SELECTOR]... [flags]
```

### Examples

```

	# Validate all resources in the default directory
	gcx resources validate

	# Validate a single resource kind
	gcx resources validate dashboards

	# Validate a multiple resource kinds
	gcx resources validate dashboards folders

	# Displaying validation results as YAML
	gcx resources validate -o yaml

	# Displaying validation results as JSON
	gcx resources validate -o json

```

### Options

```
  -h, --help                 help for validate
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --max-concurrent int   Maximum number of concurrent operations (default 10)
      --on-error string      How to handle errors during resource operations:
                               ignore — continue processing all resources and exit 0
                               fail   — continue processing all resources and exit 1 if any failed (default)
                               abort  — stop on the first error and exit 1 (default "fail")
  -o, --output string        Output format. One of: json, text, yaml (default "text")
  -p, --path strings         Paths on disk from which to read the resources. (default [./resources])
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx resources](gcx_resources.md)	 - Manipulate Grafana resources

