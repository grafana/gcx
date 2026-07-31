## gcx config unset

Unset a single value in a configuration file

### Synopsis

Unset a single value in a configuration file.

PROPERTY_NAME is a dot-delimited reference to the value to unset. It can either represent a field or a map entry.

Paths are literal: they name the exact location in the configuration file, starting from a top-level section ("stacks.<name>.", "cloud.<entry>.", "contexts.<name>.", "resources.", "current-context"). Nothing is resolved against the current context - the path you type is the path you see in "gcx config view".

```
gcx config unset PROPERTY_NAME [flags]
```

### Examples

```

	# Unset the "foo" context
	gcx config unset contexts.foo

	# Unset the "insecure-skip-verify" TLS setting on the "dev-instance" stack
	gcx config unset stacks.dev-instance.grafana.tls.insecure-skip-verify

	# Unset a cloud entry's token in the local config layer
	gcx config unset --file local cloud.grafana-com.token
```

### Options

```
      --file string     Config layer to write to (system, user, local)
  -h, --help            help for unset
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, text, yaml (default "text")
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

* [gcx config](gcx_config.md)	 - View or manipulate configuration settings

