## gcx agent skills update

Update installed gcx skills in ~/.agents/skills

### Synopsis

Update gcx-managed skills in a user-level .agents skills directory. With no skill names, gcx updates only bundled skills that are already installed locally.

```
gcx agent skills update [SKILL]... [flags]
```

### Examples

```
  gcx agent skills update
  gcx agent skills update --dry-run
  gcx agent skills update setup-gcx debug-with-grafana
```

### Options

```
      --dir string      Root directory for the .agents installation (default "~/.agents")
      --dry-run         Preview the update without writing files
  -h, --help            help for update
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, text, yaml (default "text")
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx agent skills](gcx_agent_skills.md)	 - Manage portable gcx Agent Skills

