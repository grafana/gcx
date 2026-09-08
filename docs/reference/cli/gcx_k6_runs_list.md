## gcx k6 runs list

List k6 Cloud test runs.

### Synopsis

List k6 Cloud test runs. With no argument, list runs across the stack. With an ID or name argument, or with --id, list runs for one load test. The --created-after and --created-before flags apply only to the global list.

```
gcx k6 runs list [id-or-name] [flags]
```

### Examples

```
  gcx k6 runs list
  gcx k6 runs list --created-after 2026-09-01T00:00:00Z --limit 50
  gcx k6 runs list 12345
```

### Options

```
      --created-after string    Include runs created after this RFC3339 time
      --created-before string   Include runs created before this RFC3339 time
  -h, --help                    help for list
      --id int                  Load test ID (skip name lookup)
      --jq string               jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string             Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int               Maximum number of runs to return (0 for all) (default 50)
  -o, --output string           Output format. One of: agents, json, table, yaml (default "table")
      --project-id int          Project ID (required when looking up by name)
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

* [gcx k6 runs](gcx_k6_runs.md)	 - Manage k6 test runs.

