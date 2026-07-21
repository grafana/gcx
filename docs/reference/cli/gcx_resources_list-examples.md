## gcx resources list-examples

List example manifests for resource types

### Synopsis

List example manifests for provider-backed resource types. Without arguments, lists all resources that have examples. With a selector, shows examples for matching resources.

```
gcx resources list-examples [RESOURCE_SELECTOR] [flags]
```

### Examples

```

	gcx resources list-examples
	gcx resources list-examples -o wide
	gcx resources list-examples -o json
	gcx resources list-examples -o yaml
	gcx resources list-examples incidents
	gcx resources list-examples incidents -o json
	gcx resources list-examples slo -o yaml

```

### Options

```
  -h, --help            help for list-examples
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
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

