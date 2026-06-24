## gcx instrumentation clusters list

List all clusters with their instrumentation status

### Synopsis

List all clusters with their K8s monitoring configuration and observed status.

Merges RunK8sMonitoring with ListPipelines to surface clusters that have been
configured but whose Alloy collector has not yet started reporting (pre-Alloy
clusters appear with PENDING_INSTRUMENTATION status).

For each cluster, the declared configuration is fetched concurrently via
GetK8SInstrumentation (up to 10 concurrent requests).

```
gcx instrumentation clusters list [flags]
```

### Examples

```
  # List all clusters (table output)
  gcx instrumentation clusters list

  # List in wide format (shows all flag columns)
  gcx instrumentation clusters list -o wide

  # Output as JSON
  gcx instrumentation clusters list -o json
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
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx instrumentation clusters](gcx_instrumentation_clusters.md)	 - Manage K8s monitoring configuration for clusters

