## gcx sigil templates list

List eval templates.

```
gcx sigil templates list [flags]
```

### Examples

```
  # List all templates.
  gcx sigil templates list

  # Filter by scope.
  gcx sigil templates list --scope global
```

### Options

```
  -h, --help            help for list
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: json, table, wide, yaml (default "table")
      --scope string    Filter by scope: "global" or "tenant"
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx sigil templates](gcx_sigil_templates.md)	 - Browse reusable evaluator blueprints (global and tenant-scoped).

