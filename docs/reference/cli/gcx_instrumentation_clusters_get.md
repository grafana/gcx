## gcx instrumentation clusters get

Show declared config and observed status for a cluster

### Synopsis

Show the declared K8s monitoring configuration and observed instrumentation
status for a single cluster.

The declared configuration is fetched via GetK8SInstrumentation. The observed
status is cross-referenced with RunK8sMonitoring. If the cluster is absent from
RunK8sMonitoring, ListPipelines is checked: a K8s monitoring pipeline present
means PENDING_INSTRUMENTATION; absent means NOT_INSTRUMENTED.

```
gcx instrumentation clusters get <cluster> [flags]
```

### Examples

```
  # Get cluster "prod-eu" in table format
  gcx instrumentation clusters get prod-eu

  # Get in JSON format
  gcx instrumentation clusters get prod-eu -o json
```

### Options

```
  -h, --help            help for get
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

