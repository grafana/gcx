## gcx instrumentation clusters wait

Wait until a cluster reaches INSTRUMENTED status

### Synopsis

Poll until the specified cluster reaches INSTRUMENTED status.

The command polls RunK8sMonitoring every 5 seconds. Before starting
the polling loop, it performs a pre-flight check to verify the cluster has
been declared (via gcx instrumentation setup) — if not configured, it
returns an error immediately with a remediation hint.

Exit codes:
  0  Cluster reached INSTRUMENTED status (or a non-error terminal state)
  1  Timeout reached before INSTRUMENTED status
  1  INSTRUMENTATION_ERROR status observed
  1  Pre-flight check failed (cluster not declared)

```
gcx instrumentation clusters wait <cluster> [flags]
```

### Examples

```
  # Wait with default 5-minute timeout
  gcx instrumentation clusters wait prod-eu

  # Wait with a custom timeout
  gcx instrumentation clusters wait prod-eu --timeout 10m
```

### Options

```
  -h, --help               help for wait
      --timeout duration   Maximum time to wait for INSTRUMENTED status (default 5m0s)
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use (overrides current-context in config)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx instrumentation clusters](gcx_instrumentation_clusters.md)	 - Manage K8s monitoring configuration for clusters

