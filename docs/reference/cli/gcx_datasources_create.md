## gcx datasources create

Create a datasource from a manifest file

### Synopsis

Create a datasource instance from a declarative manifest file.

The manifest is a Kubernetes-style envelope. spec.type is the plugin ID
(e.g. grafana-sentry-datasource) and selects the API group. Secret values go
in the top-level secure block via {create: <value>}, {fromEnv: <VAR>}, or
{fromFile: <path>}; they are never returned on read.

```
gcx datasources create [flags]
```

### Examples

```

	# Create a datasource from a YAML manifest
	gcx datasources create -f sentry.yaml

	# Create from stdin
	cat sentry.yaml | gcx datasources create -f -

	# Preview without writing
	gcx datasources create -f sentry.yaml --dry-run
```

### Options

```
      --config string         Path to the configuration file to use
      --context string        Name of the context to use
      --dry-run               Render the object that would be created without writing it
  -f, --filename string       File containing the datasource manifest (use - for stdin)
  -h, --help                  help for create
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

