## gcx dev lint new

Creates a new linter rule

### Synopsis

Creates a new linter rule.

```
gcx dev lint new RESOURCE_TYPE NAME [flags]
```

### Examples

```

	# Creates a new dashboard linter rule in the current directory:

	gcx dev lint new dashboard test-linter

	# Creates a new dashboard linter rule in another directory:

	gcx dev lint new dashboard test-linter -o custom-rules

```

### Options

```
  -c, --category string   Rule category (default "idiomatic")
  -h, --help              help for new
  -o, --output string     Output directory
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

* [gcx dev lint](gcx_dev_lint.md)	 - Lint Grafana resources

