## gcx dev lint run

Lint Grafana resources

### Synopsis

Lint Grafana resources.

```
gcx dev lint run PATH... [flags]
```

### Examples

```

	# Lint Grafana resources using builtin rules:

	gcx dev lint run ./resources

	# Lint specific files:

	gcx dev lint run ./resources/file.json ./resources/other.yaml

	# Display compact results:

	gcx dev lint run ./resources -o compact

	# Use custom rules:

	gcx dev lint run --rules ./custom-rules ./resources

	# Disable all rules for a resource type:

	gcx dev lint run --disable-resource dashboard ./resources

	# Disable all rules in a category:

	gcx dev lint run --disable-category idiomatic ./resources

	# Disable specific rules:

	gcx dev lint run --disable uneditable-dashboard --disable panel-title-description ./resources

	# Enable rules for specific resource types:

	gcx dev lint run --disable-all --enable-resource dashboard ./resources

	# Enable only some categories:

	gcx dev lint run --disable-all --enable-category idiomatic ./resources

	# Enable only specific rules:

	gcx dev lint run --disable-all --enable uneditable-dashboard ./resources

```

### Options

```
      --debug                          Enable debug mode
      --disable stringArray            Disable a rule
      --disable-all                    Disable all rules
      --disable-category stringArray   Disable all rules in a category
      --disable-resource stringArray   Disable all rules for a resource type
      --enable stringArray             Enable a rule
      --enable-all                     Enable all rules
      --enable-category stringArray    Enable all rules in a category
      --enable-resource stringArray    Enable all rules for a resource type
  -h, --help                           help for run
      --json string                    Comma-separated list of fields to include in JSON output, or '?' to discover available fields
      --max-concurrent int             Maximum number of concurrent operations (default 10)
  -o, --output string                  Output format. One of: compact, json, pretty, yaml (default "pretty")
  -r, --rules stringArray              Path to custom rules
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

