## gcx config unset

Unset a single value in a configuration file

### Synopsis

Unset a single value in a configuration file.

PROPERTY_NAME is a dot-delimited reference to the value to unset. It can either represent a field or a map entry.

A bare path is resolved against the current context: "datasources.prometheus" targets the context itself, while stack-owned fields ("grafana.server", "providers.slo.org-id", "slug") resolve through the context's stack reference to "stacks.<name>.<path>". Use a fully qualified path (starting with "contexts.", "stacks.", or "cloud.") to target a specific entry.

```
gcx config unset PROPERTY_NAME [flags]
```

### Examples

```

	# Unset the "foo" context
	gcx config unset contexts.foo

	# Unset the "insecure-skip-tls-verify" flag in the current context's stack
	gcx config unset grafana.insecure-skip-tls-verify

	# Unset the "insecure-skip-tls-verify" flag on the "dev-instance" stack
	gcx config unset stacks.dev-instance.grafana.insecure-skip-tls-verify

	# Unset a cloud entry's token in the local config layer
	gcx config unset --file local cloud.grafana-com.token
```

### Options

```
      --file string   Config layer to write to (system, user, local)
  -h, --help          help for unset
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

