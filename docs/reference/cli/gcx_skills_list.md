## gcx skills list

List skills bundled with the gcx binary

### Synopsis

List skills bundled with the gcx binary, including each skill's short description and install status.

```
gcx skills list [flags]
```

### Examples

```
  gcx skills list
  gcx skills list -o json
```

### Options

```
      --dir string      Root directory for the .agents installation (used to check installed status) (default "~/.agents")
  -h, --help            help for list
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: json, text, yaml (default "text")
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --context string     Name of the context to use (overrides current-context in config)
      --limit int          Maximum number of items to return from list operations (0 for all; defaults to 50 in agent mode)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx skills](gcx_skills.md)	 - Manage portable gcx Agent Skills

