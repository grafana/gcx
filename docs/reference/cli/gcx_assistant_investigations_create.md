## gcx assistant investigations create

Create a new investigation.

### Synopsis

Create a new investigation. On v2-enabled stacks, uses the v2 API with --instruction; falls back to legacy create otherwise.

```
gcx assistant investigations create [flags]
```

### Examples

```
  gcx assistant investigations create --instruction="Debug API latency spike" --team=sre
```

### Options

```
      --description string   Investigation description (legacy alias of --instruction)
  -h, --help                 help for create
      --instruction string   Investigation instruction (required on v2-enabled stacks)
      --jq string            jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string        Output format. One of: agents, json, yaml (default "yaml")
      --profile-id string    Runner profile ID (v2 only)
      --team strings         Team name to scope the investigation to (repeatable, v2 only)
      --title string         Investigation title
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx assistant investigations](gcx_assistant_investigations.md)	 - Manage Grafana Assistant investigations.

