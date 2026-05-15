## gcx synthetic-monitoring probes

Manage Synthetic Monitoring probes.

### Options

```
  -h, --help   help for probes
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

* [gcx synthetic-monitoring](gcx_synthetic-monitoring.md)	 - Manage Grafana Synthetic Monitoring checks and probes
* [gcx synthetic-monitoring probes create](gcx_synthetic-monitoring_probes_create.md)	 - Create a Synthetic Monitoring probe.
* [gcx synthetic-monitoring probes delete](gcx_synthetic-monitoring_probes_delete.md)	 - Delete Synthetic Monitoring probes.
* [gcx synthetic-monitoring probes deploy](gcx_synthetic-monitoring_probes_deploy.md)	 - Generate Kubernetes manifests for deploying an SM agent.
* [gcx synthetic-monitoring probes list](gcx_synthetic-monitoring_probes_list.md)	 - List Synthetic Monitoring probes.
* [gcx synthetic-monitoring probes token-reset](gcx_synthetic-monitoring_probes_token-reset.md)	 - Reset the auth token of a Synthetic Monitoring probe.

