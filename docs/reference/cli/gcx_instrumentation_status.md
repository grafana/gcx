## gcx instrumentation status

Show observed instrumentation state for clusters and namespaces.

### Synopsis

Show observed instrumentation state across all clusters, or narrow to a
specific cluster or namespace.

Without flags, lists all clusters including pre-Alloy clusters that have been
configured but whose Alloy collector has not yet started reporting (shown as
PENDING_INSTRUMENTATION).

Use --cluster to filter to a single cluster. Add --namespace to drill down to
workload-level status for a specific namespace, powered by RunK8sDiscovery.

```
gcx instrumentation status [flags]
```

### Options

```
      --cluster string     Filter output to a specific cluster
  -h, --help               help for status
      --json string        Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --namespace string   Filter to a specific namespace; switches to workload-level view
  -o, --output string      Output format. One of: agents, json, yaml (default "table")
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

* [gcx instrumentation](gcx_instrumentation.md)	 - Manage Grafana Instrumentation Hub

