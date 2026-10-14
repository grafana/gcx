## gcx alert template-groups update

Update or rename the selected Grafana-managed template group.

### Synopsis

The argument selects the current title; the manifest supplies the desired title.
The manifest must retain metadata.resourceVersion from get. Template kind cannot
change. Server conflicts are preserved; fetch the current manifest and reapply
edits to retry. After rename, fetch the new title or pull again to synchronize
local files.

```
gcx alert template-groups update <current-title> -f <file> [flags]
```

### Examples

```
  gcx alert template-groups get shared --kind grafana > group.yaml
  gcx alert template-groups update shared -f group.yaml
  cat group.yaml | gcx alert template-groups update shared -f - -o json
```

### Options

```
      --api-version string   Must match the manifest's apiVersion when set; the manifest decides the version
  -f, --filename string      Path to a native JSON/YAML TemplateGroup manifest ('-' reads stdin)
  -h, --help                 help for update
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

