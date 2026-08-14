## gcx irm oncall webhooks presets list

List webhook preset IDs (e.g. grafana_assistant) and their allowed triggers.

### Synopsis

List the presets that an outgoing webhook accepts. A preset fills a group of webhook fields, and it limits the trigger types of the webhook. Put the preset ID in the preset field of a webhook manifest.

```
gcx irm oncall webhooks presets list [flags]
```

### Examples

```
  # List the webhook presets
  gcx irm oncall webhooks list-presets

  # Read the trigger types that one preset allows
  gcx irm oncall webhooks list-presets -o json | jq -r '.[] | select(.id == "grafana_assistant") | .trigger_types'

  # Put the preset ID in the preset field of webhook.yaml, then create the webhook
  gcx irm oncall webhooks create -f webhook.yaml
```

### Options

```
  -h, --help            help for list
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, table, yaml (default "table")
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

* [gcx irm oncall webhooks presets](gcx_irm_oncall_webhooks_presets.md)	 - Discover webhook configuration presets.

