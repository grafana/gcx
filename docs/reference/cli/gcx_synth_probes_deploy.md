## gcx synth probes deploy

Generate Kubernetes manifests for deploying an SM agent.

```
gcx synth probes deploy [flags]
```

### Examples

```
  # Generate manifests for a probe deployment.
  gcx synth probes deploy --probe-name my-probe --token <token> --api-server-url synthetic-monitoring-grpc.grafana.net:443

  # Pipe directly into kubectl.
  gcx synth probes deploy --probe-name my-probe --token <token> --api-server-url synthetic-monitoring-grpc.grafana.net:443 | kubectl apply -f -
```

### Options

```
      --api-server-url string   SM API gRPC endpoint (required)
  -h, --help                    help for deploy
      --image string            SM agent container image (default "grafana/synthetic-monitoring-agent:latest")
      --namespace string        K8s namespace (default "synthetic-monitoring")
      --probe-name string       Name for the k8s resources (required)
      --token string            Probe auth token (required)
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --limit int          Maximum number of items to return from list operations (0 for all; defaults to 50 in agent mode)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx synth probes](gcx_synth_probes.md)	 - Manage Synthetic Monitoring probes.

