## gcx alert template-groups get

Get a native template group manifest by exact title.

```
gcx alert template-groups get <title> [flags]
```

### Examples

```
  gcx alert template-groups get shared
  gcx alert template-groups get shared --kind mimir
  gcx alert template-groups get shared --kind grafana -o json
```

### Options

```
      --api-version string   API version to use (e.g. notifications.alerting.grafana.app/v1beta1); defaults to the server's preferred version
  -h, --help                 help for get
      --jq string            jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --kind string          Select grafana or mimir when titles overlap; omitted requires a unique title
  -o, --output string        Output format. One of: agents, json, table, wide, yaml (default "yaml")
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from known agent identity variables. Set GCX_AGENT_NAME to identify a supported harness, or GCX_AGENT_MODE to control the mode.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx alert template-groups](gcx_alert_template-groups.md)	 - Manage native notification template groups by title.

