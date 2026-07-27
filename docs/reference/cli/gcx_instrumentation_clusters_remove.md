## gcx instrumentation clusters remove

Remove K8s monitoring from a cluster

### Synopsis

Remove K8s monitoring from a cluster.

Calls SetK8SInstrumentation with Selection=SELECTION_EXCLUDED. The backend
interprets this as a request to delete the K8s monitoring pipeline for the
cluster.

IMPORTANT: After removing, the cluster's status takes approximately 5 minutes
to transition from INSTRUMENTED to NOT_INSTRUMENTED. During this decay window,
the cluster may still appear as INSTRUMENTED in status output. This is expected
behaviour — the Alloy collector drains its in-flight telemetry before stopping.

Requires --yes to confirm the destructive operation.

```
gcx instrumentation clusters remove <cluster> [flags]
```

### Examples

```
  # Remove K8s monitoring for cluster "prod-eu"
  gcx instrumentation clusters remove prod-eu --yes
```

### Options

```
  -h, --help            help for remove
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, text, yaml (default "text")
      --yes             Confirm the remove operation (required)
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

