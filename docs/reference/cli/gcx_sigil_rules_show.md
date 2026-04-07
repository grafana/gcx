## gcx sigil rules show

Show evaluation rules or a single rule detail.

### Synopsis

Show evaluation rules. Without an ID, lists all rules.
With an ID, shows the full rule definition.

```
gcx sigil rules show [rule-id] [flags]
```

### Options

```
  -h, --help            help for show
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: json, table, wide, yaml (default "table")
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

* [gcx sigil rules](gcx_sigil_rules.md)	 - Query Sigil evaluation rules.

