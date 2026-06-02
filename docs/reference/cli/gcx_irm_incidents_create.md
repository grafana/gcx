## gcx irm incidents create

Create a new incident from a file.

```
gcx irm incidents create [flags]
```

### Examples

```
  # Create an incident from a YAML manifest:
  cat <<EOF | gcx irm incidents create -f -
  apiVersion: incident.ext.grafana.app/v1alpha1
  kind: Incident
  metadata:
    name: my-incident
  spec:
    title: "Service degradation in production"
    status: active
    isDrill: false
    incidentType: internal
    labels:
      - key: team
        label: platform
      - key: env
        label: production
  EOF

  # Create from a file:
  gcx irm incidents create -f incident.yaml
```

### Options

```
  -f, --filename string   File containing the incident manifest (use - for stdin)
  -h, --help              help for create
      --json string       Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string     Output format. One of: agents, json, yaml (default "yaml")
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

* [gcx irm incidents](gcx_irm_incidents.md)	 - Manage incidents.

