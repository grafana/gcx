## gcx instrumentation services clear

Remove a per-workload override, inheriting namespace default

### Synopsis

Remove any per-workload inclusion or exclusion override for a workload.

After clearing, the workload inherits the namespace autoinstrument default.

The operation is idempotent: if no override exists for the workload,
the command exits 0 without making any backend calls.

The write uses an optimistic-lock guard (rmw.Update) when a change is needed:
if the namespace list changes between the initial read and the pre-write re-check,
the command returns a conflict error and must be retried.

Examples:
  gcx instrumentation services clear prod-1 checkout frontend

```
gcx instrumentation services clear <cluster> <namespace> <service> [flags]
```

### Options

```
  -h, --help   help for clear
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

