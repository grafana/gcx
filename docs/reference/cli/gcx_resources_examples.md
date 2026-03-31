## gcx resources examples

List example manifests for resource types

### Synopsis

List example manifests for provider-backed resource types. Without arguments, lists all resources that have examples. With a selector, shows examples for matching resources.

```
gcx resources examples [RESOURCE_SELECTOR] [flags]
```

### Examples

```

	gcx resources examples
	gcx resources examples -o wide
	gcx resources examples -o json
	gcx resources examples -o yaml
	gcx resources examples incidents
	gcx resources examples incidents -o json
	gcx resources examples slo -o yaml

```

### Options

```
  -h, --help            help for examples
      --json string     Comma-separated list of fields to include in JSON output, or '?' to discover available fields
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

