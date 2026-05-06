## gcx instrumentation clusters apps configure

Configure Beyla instrumentation for a namespace

### Synopsis

Configure Beyla auto-instrumentation for a namespace within the given cluster.

Two mutually exclusive modes:

  --use-defaults --yes
      Apply all-on canonical defaults, overwriting current state. Requires --yes.
      Defaults: autoinstrument=true, all signals enabled.

  --<feat>[=true|false] (one or more)
      Set listed features; unspecified features preserve their current value (RMW).
      Idempotent. No confirmation required.

Combining --use-defaults with any --<feat> flag is an error.

```
gcx instrumentation clusters apps configure <cluster> <namespace> [flags]
```

### Options

```
      --extended-metrics   Set extended Beyla metrics collection. Pass --extended-metrics=false to disable.
  -h, --help               help for configure
      --logging            Set log collection. Pass --logging=false to disable.
      --process-metrics    Set process-level metrics collection. Pass --process-metrics=false to disable.
      --profiling          Set continuous profiling collection. Pass --profiling=false to disable.
      --tracing            Set distributed tracing collection. Pass --tracing=false to disable.
      --use-defaults       Apply all-on canonical defaults. Requires --yes.
      --yes                Confirm the --use-defaults operation (required with --use-defaults)
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

