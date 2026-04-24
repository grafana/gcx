## gcx skills update

Update installed gcx skills in ~/.agents/skills

### Synopsis

Update gcx-managed skills in a user-level .agents skills directory. With no skill names, gcx updates only bundled skills that are already installed locally.

```
gcx skills update [SKILL]... [flags]
```

### Examples

```
  gcx skills update
  gcx skills update --dry-run
  gcx skills update setup-gcx explore-datasources
```

### Options

```
      --dir string      Root directory for the .agents installation (default "~/.agents")
      --dry-run         Preview the update without writing files
  -h, --help            help for update
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: json, text, yaml (default "text")
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --context string     Name of the context to use (overrides current-context in config)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx skills](gcx_skills.md)	 - Manage portable gcx Agent Skills

