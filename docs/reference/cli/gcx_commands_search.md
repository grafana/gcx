## gcx commands search

Find CLI commands by intent using local text search

### Synopsis

Search the installed CLI's command paths, aliases, descriptions and parameters.
Quote a task description to receive up to five ranked suggestions. Matching uses
case-insensitive words, prefixes and single-character typo correction, not semantic
understanding. Commands matching more query words rank above partial matches.
Suggestions may only match part of your query; inspect the selected command with
--help before using it. No Grafana connection or credentials are required, and
suggestions are not checked for availability in your current context.

Use --limit 0 for all matches or help-tree to browse a known command group.

```
gcx commands search <query> [flags]
```

### Examples

```
  gcx commands search "create an uptime check"
  gcx commands search "export dashboards" --limit 10
  gcx commands search "query metrics" -o json
```

### Options

```
  -h, --help            help for search
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int       Maximum number of command suggestions to return. 0 means all results are returned (default 5)
  -o, --output string   Output format. One of: agents, json, text, yaml (default "text")
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx commands](gcx_commands.md)	 - List all commands with rich metadata for agent consumption

