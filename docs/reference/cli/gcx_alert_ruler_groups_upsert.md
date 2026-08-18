## gcx alert ruler groups upsert

Create or update ruler rule groups from a file.

### Synopsis

Create or update rule groups in a ruler namespace. The input may be a
standard Prometheus rules file (with a top-level "groups:" list) or a single
bare rule group. Upserting a group replaces the group with the same name.

```
gcx alert ruler groups upsert NAMESPACE [flags]
```

### Options

```
  -d, --datasource string   UID of the Mimir or Loki datasource used as ruler (required)
      --dry-run             Parse and validate only; send nothing to the ruler
  -f, --filename string     File containing rule groups (Prometheus rules file or a single group; YAML/JSON, use - for stdin)
  -h, --help                help for upsert
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, json, text, yaml (default "text")
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, CODEX_THREAD_ID, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx alert ruler groups](gcx_alert_ruler_groups.md)	 - Manage datasource-managed (Mimir/Loki ruler) rule groups.

