## gcx agent skills

Manage portable gcx Agent Skills

### Synopsis

Install the canonical portable gcx Agent Skills bundle for .agents-compatible agent harnesses.

### Options

```
  -h, --help   help for skills
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

* [gcx agent](gcx_agent.md)	 - Agent mode utilities
* [gcx agent skills get](gcx_agent_skills_get.md)	 - Print a bundled skill's content without installing it
* [gcx agent skills install](gcx_agent_skills_install.md)	 - Install bundled gcx skills into ~/.agents/skills
* [gcx agent skills list](gcx_agent_skills_list.md)	 - List skills bundled with the gcx binary
* [gcx agent skills uninstall](gcx_agent_skills_uninstall.md)	 - Uninstall gcx-managed skills from ~/.agents/skills
* [gcx agent skills update](gcx_agent_skills_update.md)	 - Update installed gcx skills in ~/.agents/skills

