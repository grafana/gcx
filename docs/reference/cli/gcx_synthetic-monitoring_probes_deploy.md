## gcx synthetic-monitoring probes deploy

Generate Kubernetes manifests for deploying an SM agent.

### Synopsis

Generate a Namespace, Secret, and Deployment for a private Synthetic Monitoring probe. The Deployment reads the probe token from the Secret and sends it to the agent through a protected environment variable. The output contains the Secret, so store saved output securely.

```
gcx synthetic-monitoring probes deploy [flags]
```

### Examples

```
  # Read the probe token from a file.
  gcx synthetic-monitoring probes deploy --probe-name my-probe --token-file ./probe-token --api-server-url synthetic-monitoring-grpc.grafana.net:443

  # Read the probe token from an environment variable and apply the manifests.
  gcx synthetic-monitoring probes deploy --probe-name my-probe --token-env PROBE_TOKEN --api-server-url synthetic-monitoring-grpc.grafana.net:443 | kubectl apply -f -

  # Read the probe token from standard input.
  printf '%s' "$PROBE_TOKEN" | gcx synthetic-monitoring probes deploy --probe-name my-probe --token-file - --api-server-url synthetic-monitoring-grpc.grafana.net:443
```

### Options

```
      --api-server-url string   SM API gRPC address in host:port format (required)
  -h, --help                    help for deploy
      --image string            SM agent container image (default "grafana/synthetic-monitoring-agent:latest")
      --namespace string        Kubernetes namespace (default "synthetic-monitoring")
      --probe-name string       Name for the Kubernetes resources (required)
      --token-env string        Read the probe auth token from this environment variable
      --token-file string       Read the probe auth token from a file (- for standard input)
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx synthetic-monitoring probes](gcx_synthetic-monitoring_probes.md)	 - Manage Synthetic Monitoring probes.

