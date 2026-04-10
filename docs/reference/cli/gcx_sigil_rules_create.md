## gcx sigil rules create

Create an evaluation rule from a file.

```
gcx sigil rules create [flags]
```

### Examples

```
  # Create a rule from a YAML file.
  gcx sigil rules create -f rule.yaml

  # Create from stdin.
  gcx sigil rules create -f -

  # Create and output as YAML.
  gcx sigil rules create -f rule.json -o yaml
```

### Options

```
  -f, --filename string   File containing the rule definition (use - for stdin)
  -h, --help              help for create
      --json string       Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string     Output format. One of: json, yaml (default "json")
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

* [gcx sigil rules](gcx_sigil_rules.md)	 - Manage rules that route generations to evaluators.

