## gcx alert routing-trees get

Get a routing tree manifest by name.

### Synopsis

Get a routing tree's native manifest by name. The default tree is
"user-defined". The name is sent to the server as given.

```
gcx alert routing-trees get <name> [flags]
```

### Options

```
      --api-version string   API version to use (e.g. notifications.alerting.grafana.app/v1beta1); defaults to the server's preferred version
  -h, --help                 help for get
      --jq string            jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string        Output format. One of: agents, json, yaml (default "yaml")
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx alert routing-trees](gcx_alert_routing-trees.md)	 - Manage notification routing trees (default and named).

