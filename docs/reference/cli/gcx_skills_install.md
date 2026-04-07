## gcx skills install

Install a bundled skill into your local skills directory

```
gcx skills install <skill-name> [flags]
```

### Options

```
      --dir string      Install directory for skills (default "/Users/annanay/.claude/skills")
      --force           Overwrite the destination skill directory if it already exists
  -h, --help            help for install
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: json, text, yaml (default "text")
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --context string   Name of the context to use (overrides current-context in config)
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx skills](gcx_skills.md)	 - List and install bundled gcx agent skills

