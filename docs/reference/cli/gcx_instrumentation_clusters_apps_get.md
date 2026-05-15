## gcx instrumentation clusters apps get

Get the app instrumentation entry for a single namespace

### Synopsis

Get the declared Beyla instrumentation configuration for a single namespace
within the given cluster.

Reads declared state from GetAppInstrumentation and filters client-side to the
requested namespace. Exits non-zero with a not-found error when the
namespace has no declared configuration.

```
gcx instrumentation clusters apps get <cluster> <namespace> [flags]
```

### Options

```
  -h, --help            help for get
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, text, wide, yaml (default "text")
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

* [gcx instrumentation clusters apps](gcx_instrumentation_clusters_apps.md)	 - Manage namespace-level Beyla instrumentation for a cluster

