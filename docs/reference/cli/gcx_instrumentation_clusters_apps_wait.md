## gcx instrumentation clusters apps wait

Wait for namespace instrumentation to reach a stable state

### Synopsis

Wait for the namespace Beyla instrumentation to transition out of a pending
state by polling RunK8sDiscovery at 5-second intervals.

Exit 0 when the namespace's workloads reach a stable non-pending state
(INSTRUMENTED, NOT_INSTRUMENTED, EXCLUDED, or any terminal non-error state).

Exit non-zero when:
  - INSTRUMENTATION_ERROR is observed for any workload.
  - The --timeout duration elapses while still in a pending state.

```
gcx instrumentation clusters apps wait <cluster> <namespace> [flags]
```

### Options

```
  -h, --help               help for wait
      --timeout duration   Maximum time to wait (e.g. 5m, 10m, 1h) (default 5m0s)
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

* [gcx instrumentation clusters apps](gcx_instrumentation_clusters_apps.md)	 - Manage namespace-level Beyla instrumentation for a cluster

