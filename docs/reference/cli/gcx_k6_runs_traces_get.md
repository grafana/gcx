## gcx k6 runs traces get

[experimental] Get a browser trace for a k6 test run.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.

Get one full OTLP browser trace. Use the trace ID from gcx k6 runs traces list.

```
gcx k6 runs traces get <run-id> <trace-id> [flags]
```

### Examples

```
  gcx k6 runs traces get 12345 0123456789abcdef0123456789abcdef
  gcx k6 runs traces get 12345 0123456789abcdef0123456789abcdef --json batches
```

### Options

```
  -h, --help            help for get
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, yaml (default "json")
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

* [gcx k6 runs traces](gcx_k6_runs_traces.md)	 - [experimental] Inspect browser traces for k6 test runs.

