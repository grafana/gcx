## gcx agent skills list

List skills bundled with the gcx binary

### Synopsis

List skills bundled with the gcx binary, including each skill's short description and install status.

```
gcx agent skills list [flags]
```

### Examples

```
  gcx agent skills list
  gcx agent skills list -o json
```

### Options

```
      --dir string      Root directory for the .agents installation (used to check installed status) (default "~/.agents")
  -h, --help            help for list
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

