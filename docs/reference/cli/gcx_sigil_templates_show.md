## gcx sigil templates show

Show eval templates or a single template detail.

### Synopsis

Show eval templates. Without an ID, lists all templates.
With an ID, shows the full template definition including config and output keys.

Templates are reusable evaluator blueprints. Export a template as YAML,
customize it, and create an evaluator with 'evaluators create -f'.

```
gcx sigil templates show [template-id] [flags]
```

### Examples

```
  # List all templates.
  gcx sigil templates show

  # Show a template's config and output keys.
  gcx sigil templates show my-template

  # Filter by scope.
  gcx sigil templates show --scope global

  # Export a template and create an evaluator from it.
  gcx sigil templates show my-template -o yaml > evaluator.yaml
  gcx sigil evaluators create -f evaluator.yaml
```

### Options

```
  -h, --help            help for show
      --json string     Comma-separated list of fields to include in JSON output, or '?' to discover available fields
  -o, --output string   Output format. One of: json, table, wide, yaml (default "table")
      --scope string    Filter by scope: "global" or "tenant"
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

* [gcx sigil templates](gcx_sigil_templates.md)	 - Browse reusable evaluator blueprints (global and tenant-scoped).

