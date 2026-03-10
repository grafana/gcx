## grafanactl linter test

Run linter rules tests

### Synopsis

Run linter rules tests.

```
grafanactl linter test PATH... [flags]
```

### Examples

```

	# Run all tests in a directory:

	grafanactl linter test ./internal/linter/bundle/grafanactl/

```

### Options

```
      --bundle             Enable bundle mode
      --coverage           Report coverage
      --debug              Enable debug mode
  -h, --help               help for test
      --ignore strings     File and directory names to ignore during loading (e.g., '.*' excludes hidden files)
  -o, --output string      Output format. One of: json, pretty (default "pretty")
      --run string         Run only test cases matching the regular expression
      --timeout duration   Set test timeout
```

### Options inherited from parent commands

```
      --agent           Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GRAFANACTL_AGENT_MODE env vars.
      --no-color        Disable color output
      --no-truncate     Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count   Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [grafanactl linter](grafanactl_linter.md)	 - Lint Grafana resources

