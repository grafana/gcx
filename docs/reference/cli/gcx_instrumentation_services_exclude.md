## gcx instrumentation services exclude

Exclude a workload from instrumentation (DWIM, idempotent)

### Synopsis

Exclude a specific workload from instrumentation using DWIM semantics.

DWIM logic:
  - Removes any existing INCLUDED override for the workload.
  - Adds an EXCLUDED override iff the namespace autoinstrument is explicitly
    true (i.e. the namespace default is on, so an explicit opt-out is needed).
  - If the namespace autoinstrument is false/nil, no override is added (the
    namespace default is already off — adding EXCLUDED would be redundant).

The operation is idempotent: running it twice with the same args exits 0 and
the second call is a no-op against the backend.

The write uses an optimistic-lock guard (rmw.Update): if the namespace list
changes between the initial read and the pre-write re-check, the command returns
a conflict error and must be retried.

Examples:
  gcx instrumentation services exclude prod-1 checkout payment-svc

```
gcx instrumentation services exclude <cluster> <namespace> <service> [flags]
```

### Options

```
  -h, --help   help for exclude
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

* [gcx instrumentation services](gcx_instrumentation_services.md)	 - Manage workload-level instrumentation across the fleet

