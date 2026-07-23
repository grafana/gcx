## gcx agent prune

Remove gcx agent spill files older than 30 minutes

### Synopsis

Remove gcx agent spill files (gcx-results-*.json) from the system temp directory that are older than 30 minutes.

These files are created when a command response exceeds the spill threshold (default 100 KiB). Run prune periodically to keep the temp directory clean, or call it at the end of an agent session.

```
gcx agent prune [flags]
```

### Options

```
  -h, --help            help for prune
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

* [gcx agent](gcx_agent.md)	 - Agent mode utilities

