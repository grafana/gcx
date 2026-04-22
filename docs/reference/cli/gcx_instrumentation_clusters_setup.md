## gcx instrumentation clusters setup

Interactive bootstrap for a cluster's instrumentation configuration.

### Synopsis

Bootstrap a cluster's instrumentation configuration through a guided flow.

Prompts for the cluster name (when not provided as a positional argument) and
Kubernetes monitoring flags (cost metrics, cluster events, node logs, energy
metrics), then prints the Helm chart installation instructions.

Use --defaults / -y to accept all defaults without prompting (CI-friendly);
in that mode the cluster name must be passed as a positional argument.

```
gcx instrumentation clusters setup [cluster] [flags]
```

### Options

```
  -y, --defaults   Accept all defaults without prompting (non-interactive / CI mode)
  -h, --help       help for setup
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

* [gcx instrumentation clusters](gcx_instrumentation_clusters.md)	 - Manage Kubernetes cluster monitoring configuration.

