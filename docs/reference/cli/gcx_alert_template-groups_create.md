## gcx alert template-groups create

Create a Grafana-managed template group from a native manifest.

### Synopsis

Create one Grafana-managed template group. Create never overwrites an existing group.
Source server identity and concurrency metadata are omitted from the request.
Imported Mimir groups must be managed in the external Alertmanager configuration.

```
gcx alert template-groups create -f <file> [flags]
```

### Examples

```
  gcx alert template-groups create -f group.yaml
  gcx alert template-groups create -f group.json -o json
  cat group.yaml | gcx alert template-groups create -f -
```

### Options

```
      --api-version string   Must match the manifest's apiVersion when set; the manifest decides the version
  -f, --filename string      Path to a native JSON/YAML TemplateGroup manifest ('-' reads stdin)
  -h, --help                 help for create
      --jq string            jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string        Output format. One of: agents, json, text, yaml (default "text")
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

