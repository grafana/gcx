## gcx dev lint test

Run linter rule tests

### Synopsis

Run test suites for linter rules. Each rule directory should contain test fixtures and expected output files. Reports pass/fail for each test case.

```
gcx dev lint test PATH... [flags]
```

### Examples

```

	# Run all tests in a directory:

	gcx dev lint test ./internal/linter/bundle/gcx/

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
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx dev lint](gcx_dev_lint.md)	 - Lint Grafana resources

