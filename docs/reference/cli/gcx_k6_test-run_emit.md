## gcx k6 test-run emit

Fetch a k6 Cloud test and emit Kubernetes TestRun CRD manifests.

```
gcx k6 test-run emit [test-name] [flags]
```

### Options

```
      --apply                 Apply ConfigMap and TestRun manifests via kubectl
      --emit-secret           Include Secret manifest stub in output
  -h, --help                  help for emit
      --id int                Load test ID (skip name lookup)
      --namespace string      Kubernetes namespace for emitted manifests (default "k6-tests")
      --parallelism int       Number of parallel k6 runner pods (default 1)
      --project-id int        k6 Cloud project ID
      --token-secret string   Secret name for the Grafana Cloud token (default "grafana-k6-token")
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx k6 test-run](gcx_k6_test-run.md)	 - Manage k6 TestRun CRD manifests.

