## gcx config use-context

Set the current context

### Synopsis

Set the current context and update the configuration file.

Run without arguments to pick a context interactively, or pass "-" to switch
back to the previously active context.

In agent mode or when stdout is not a TTY, a context name is required.

When multiple config files are loaded (e.g. a local .gcx.yaml alongside the
user config), use --file to choose which layer to update.

```
gcx config use-context [CONTEXT_NAME] [flags]
```

### Examples

```

	gcx config use-context dev-instance
	gcx config use                 # interactive picker
	gcx config use -               # previous context

	# Update the local .gcx.yaml when both user and local configs exist
	gcx config use-context --file local dev-instance
```

### Options

```
      --file string     Config layer to write to (system, user, local)
  -h, --help            help for use-context
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

