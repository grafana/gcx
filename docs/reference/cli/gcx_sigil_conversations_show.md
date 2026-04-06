## gcx sigil conversations show

Show conversations or a single conversation detail.

### Synopsis

Show conversations. Without an ID, lists conversations (use --limit to control count).
With an ID, shows the full conversation detail including all generations.

```
gcx sigil conversations show [conversation-id] [flags]
```

### Options

```
  -h, --help            help for show
      --json string     Comma-separated list of fields to include in JSON output, or '?' to discover available fields
      --limit int       Maximum number of conversations to return (0 for no limit) (default 100)
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

* [gcx sigil conversations](gcx_sigil_conversations.md)	 - Query Sigil conversations.

