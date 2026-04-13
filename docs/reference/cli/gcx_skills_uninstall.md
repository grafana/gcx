## gcx skills uninstall

Uninstall installed skills from ~/.agents/skills

### Synopsis

Remove one or more installed skills from a user-level .agents skills directory.

```
gcx skills uninstall [SKILL]... [flags]
```

### Examples

```
  gcx skills uninstall grafanacloud-gcx
  gcx skills uninstall grafanacloud-gcx skill-installer
  gcx skills uninstall --all --yes
  gcx skills uninstall --all --yes --dry-run
```

### Options

```
      --all             Uninstall all skills in the target directory
      --dir string      Root directory for the .agents installation (default "~/.agents")
      --dry-run         Preview the uninstall without removing files
  -h, --help            help for uninstall
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: json, text, yaml (default "text")
  -y, --yes             Auto-approve uninstalling all skills
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

