## gcx api

Make raw API requests to Grafana

### Synopsis

Make raw API requests to Grafana using the configured authentication.

```
gcx api PATH [flags]
```

### Examples

```
  # List all datasources
  gcx api /api/datasources

  # Get a specific datasource by UID
  gcx api /api/datasources/uid/my-prometheus

  # Get Grafana health status
  gcx api /api/health

  # Create a folder (POST implied by -d)
  gcx api /api/folders -d '{"title":"My Folder"}'

  # Create a dashboard from a file
  gcx api /api/dashboards/db -d @dashboard.json

  # Delete a dashboard
  gcx api /api/dashboards/uid/my-dashboard -X DELETE

  # Output as YAML
  gcx api /api/datasources -o yaml
```

### Options

```
      --config string        Path to the configuration file to use
      --context string       Name of the context to use
  -d, --data string          Request body (use @file for file, @- for stdin). Implies POST.
  -H, --header stringArray   Custom headers (repeatable)
  -h, --help                 help for api
      --json string          Comma-separated list of fields to include in JSON output, or '?' to discover available fields
  -X, --method string        HTTP method (default: GET, or POST if -d is set)
  -o, --output string        Output format for JSON responses. One of: json, yaml (default "json")
```

### Options inherited from parent commands

```
      --agent           Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --no-color        Disable color output
      --no-truncate     Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count   Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - 

