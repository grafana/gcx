## grafanactl config unset

Unset an single value in a configuration file

### Synopsis

Unset an single value in a configuration file.

PROPERTY_NAME is a dot-delimited reference to the value to unset. It can either represent a field or a map entry.

```
grafanactl config unset PROPERTY_NAME [flags]
```

### Examples

```

	# Unset the "foo" context
	grafanactl config unset contexts.foo

	# Unset the "insecure-skip-tls-verify" flag in the "dev-instance" context
	grafanactl config unset contexts.dev-instance.grafana.insecure-skip-tls-verify
```

### Options

```
  -h, --help   help for unset
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

* [grafanactl config](grafanactl_config.md)	 - View or manipulate configuration settings

