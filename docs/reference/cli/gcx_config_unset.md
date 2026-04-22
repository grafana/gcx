## gcx config unset

Unset a single value in a configuration file

### Synopsis

Unset a single value in a configuration file.

PROPERTY_NAME is a dot-delimited reference to the value to unset. It can either represent a field or a map entry.

```
gcx config unset PROPERTY_NAME [flags]
```

### Examples

```

	# Unset the "foo" context
	gcx config unset contexts.foo

	# Unset the "insecure-skip-tls-verify" flag in the "dev-instance" context
	gcx config unset contexts.dev-instance.grafana.insecure-skip-tls-verify

	# Unset a value in the local config layer
	gcx config unset --file local contexts.prod.cloud.token
```

### Options

```
      --file string   Config layer to write to (system, user, local)
  -h, --help          help for unset
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --limit int          Maximum number of items to return from list operations (0 for all; defaults to 50 in agent mode)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx config](gcx_config.md)	 - View or manipulate configuration settings

