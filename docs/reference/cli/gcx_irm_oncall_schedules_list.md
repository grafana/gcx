## gcx irm oncall schedules list

List OnCall schedules.

### Synopsis

List OnCall schedules and their current assignees.

JSON output includes current assignees in spec.on_call_now, with usernames
available directly without a separate users lookup.
Use --jq to filter the returned schedules by name and select only the fields you need.
For coverage over a date range or handover times, use schedules list-final-shifts
with the schedule ID from metadata.name; on_call_now describes only the current moment.

```
gcx irm oncall schedules list [flags]
```

### Examples

```
  # List schedules and current assignees
  gcx irm oncall schedules list

  # Show only schedule names and current usernames
  gcx irm oncall schedules list --jq '[.[] | {name: .spec.name, on_call_now: [.spec.on_call_now[]? | .username]}]'

  # Find Payments schedules in a known context (name match, case-insensitive)
  gcx --context prod irm oncall schedules list --jq '[.[] | select(.spec.name | test("payments"; "i")) | {id: .metadata.name, name: .spec.name, on_call_now: [.spec.on_call_now[]? | .username]}]'
```

### Options

```
  -h, --help            help for list
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
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

* [gcx irm oncall schedules](gcx_irm_oncall_schedules.md)	 - Manage OnCall schedules.

