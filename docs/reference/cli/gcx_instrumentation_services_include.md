## gcx instrumentation services include

Include a workload for instrumentation (DWIM, idempotent)

### Synopsis

Include a specific workload for instrumentation using DWIM semantics.

DWIM logic:
  - Removes any existing EXCLUDED override for the workload.
  - Adds an INCLUDED override iff the namespace autoinstrument is NOT explicitly
    true (i.e. the namespace default is off, so an explicit opt-in is needed).
  - If the namespace autoinstrument is true, no override is added (namespace
    default is already on — adding INCLUDED would be redundant).

The operation is idempotent: running it twice with the same args exits 0
and the second call is a no-op against the backend.

The write uses an optimistic-lock guard (rmw.Update): if the namespace list
changes between the initial read and the pre-write re-check, the command returns
a conflict error and must be retried.

Examples:
  gcx instrumentation services include prod-1 checkout frontend

```
gcx instrumentation services include <cluster> <namespace> <service> [flags]
```

### Options

```
  -h, --help            help for include
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, text, yaml (default "text")
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

* [gcx instrumentation services](gcx_instrumentation_services.md)	 - Manage workload-level instrumentation across the fleet

