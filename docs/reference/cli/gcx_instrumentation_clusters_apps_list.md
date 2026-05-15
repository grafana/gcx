## gcx instrumentation clusters apps list

List all namespace app instrumentation entries for a cluster

### Synopsis

List all namespace-level Beyla instrumentation entries for the given cluster.

Reads declared state from GetAppInstrumentation (a single RPC call). The output
reflects the configuration stored in Grafana Cloud, not the live observed state.
Use "gcx instrumentation status" for observed-state status.

```
gcx instrumentation clusters apps list <cluster> [flags]
```

### Options

```
  -h, --help            help for list
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, text, wide, yaml (default "text")
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

* [gcx instrumentation clusters apps](gcx_instrumentation_clusters_apps.md)	 - Manage namespace-level Beyla instrumentation for a cluster

