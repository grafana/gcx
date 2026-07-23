## gcx config set

Set a single value in a configuration file

### Synopsis

Set a single value in a configuration file.

PROPERTY_NAME is a dot-delimited reference to the value to set. It can either represent a field or a map entry.

Paths are literal: they name the exact location in the configuration file, starting from a top-level section ("stacks.<name>.", "cloud.<entry>.", "contexts.<name>.", "resources.", "current-context"). Nothing is resolved against the current context - the path you type is the path you see in "gcx config view".

PROPERTY_VALUE is the new value to set.

```
gcx config set PROPERTY_NAME PROPERTY_VALUE [flags]
```

### Examples

```

	# Set the "server" field on the "dev-instance" stack
	gcx config set stacks.dev-instance.grafana.server https://grafana-dev.example

	# Disable the validation of the server's SSL certificate on a stack
	gcx config set stacks.dev-instance.grafana.tls.insecure-skip-verify true

	# Set the default prometheus datasource for a context
	gcx config set contexts.dev.datasources.prometheus my-prom-uid

	# Set a cloud entry's token in the local config layer
	gcx config set --file local cloud.grafana-com.token my-token
```

### Options

```
      --file string   Config layer to write to (system, user, local)
  -h, --help          help for set
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

