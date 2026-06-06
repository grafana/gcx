## gcx agent skills get

Print a bundled skill's content without installing it

### Synopsis

Print the content of a bundled gcx Agent Skill straight from the embedded bundle, without writing anything to ~/.agents.

By default the skill's SKILL.md body is printed. Pass a reference path (e.g. references/query-patterns.md) to print a single bundled reference file instead.

```
gcx agent skills get SKILL [REFERENCE] [flags]
```

### Examples

```
  gcx agent skills get create-dashboard
  gcx agent skills get create-dashboard -o json
  gcx agent skills get debug-with-grafana references/query-patterns.md
```

### Options

```
  -h, --help            help for get
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

