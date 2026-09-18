## gcx fleet collectors list

List collectors.

### Synopsis

List Fleet Management collectors and their reported health attributes.

Use --limit 0 for a complete fleet audit. Structured output includes local and
remote attributes plus the timestamps that the Fleet API reports.

```
gcx fleet collectors list [flags]
```

### Examples

```
  # List a bounded collector summary
  gcx fleet collectors list

  # Audit versions and operating systems across the complete fleet
  gcx fleet collectors list --limit 0 --json spec.id,spec.local_attributes,spec.updated_at

  # Build a compact version inventory
  gcx fleet collectors list --limit 0 --jq '[.[] | {id: .spec.id, version: .spec.local_attributes["collector.version"], os: (.spec.local_attributes["collector.os"] // .spec.local_attributes["os.type"]), updated_at: .spec.updated_at}]'
```

### Options

```
  -h, --help            help for list
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int       Maximum number of collectors to return. 0 means all results are returned (default 50)
  -o, --output string   Output format. One of: agents, json, table, wide, yaml (default "table")
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

* [gcx fleet collectors](gcx_fleet_collectors.md)	 - Manage Fleet Management collectors.

