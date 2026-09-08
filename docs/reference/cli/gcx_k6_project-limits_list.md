## gcx k6 project-limits list

List k6 Cloud project limits.

```
gcx k6 project-limits list [flags]
```

### Options

```
  -h, --help              help for list
      --jq string         jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string       Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int         Maximum number of projects to return (1 to 1000) (default 50)
  -o, --output string     Output format. One of: agents, json, table, yaml (default "table")
      --project-id ints   Project IDs to include (maximum 30)
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx k6 project-limits](gcx_k6_project-limits.md)	 - Inspect k6 Cloud project limits.

