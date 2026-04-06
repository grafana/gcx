## gcx sigil rules update

Update an evaluation rule from a file.

### Synopsis

Update an evaluation rule by patching it with fields from a JSON or YAML file.
Only the fields present in the file are updated; omitted fields are left unchanged.

```
gcx sigil rules update <rule-id> [flags]
```

### Examples

```
  # Update a rule's sample rate and evaluators.
  gcx sigil rules update my-rule -f patch.yaml
```

### Options

```
  -f, --filename string   File containing the rule fields to update (use - for stdin)
  -h, --help              help for update
      --json string       Comma-separated list of fields to include in JSON output, or '?' to discover available fields
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

