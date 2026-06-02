## gcx irm oncall alert-groups silence

Silence alert groups (single by ID, or bulk by filter).

### Synopsis

Silence alert groups.

Two forms are supported:

  - Single-target: pass a positional <id>.
  - Bulk-by-filter: omit the positional and pass one or more filter flags
    (--team, --state, --integration, --max-age, --mine, --all).

Bulk-by-filter prompts for confirmation in TTY mode when the matched count
exceeds 1; pass --force to skip the prompt. Agent mode requires --force
explicitly when count > 1 (auto-confirm of destructive bulk operations is
disabled by design).

Idempotent: re-running on an already-silenced group reports changed:false
(single-target) or summary.skipped++ (bulk) — not an error.

```
gcx irm oncall alert-groups silence [<id>] [flags]
```

### Options

```
      --all                   Bypass the default status and is_root filters
      --duration int          Silence duration in seconds (default 3600)
      --force                 Skip the count-confirmation prompt and proceed without interactive confirmation
  -h, --help                  help for silence
      --integration strings   Filter: integration PK (repeatable)
      --json string           Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --max-age string        Filter: alert groups started within this duration (e.g. 1h, 24h, 7d)
      --mine                  Filter: limit to alert groups for the authenticated user
  -o, --output string         Output format. One of: agents, json, text, yaml (default "text")
      --state strings         Filter: state (firing|acknowledged|resolved|silenced; repeatable)
      --team strings          Filter: team PK (repeatable)
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx irm oncall alert-groups](gcx_irm_oncall_alert-groups.md)	 - Manage alert groups.

