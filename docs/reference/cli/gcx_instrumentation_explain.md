## gcx instrumentation explain

Show an explanation for an otel-checker finding

### Synopsis

Show a markdown explanation document for an otel-checker finding.

Each finding emitted by `gcx instrumentation check` may carry an
explain ID (e.g. env.otel-service-name.unset). Passing that ID here prints the
full explanation — what the finding means, why it matters, and how to fix it.

To see every available ID, run `gcx instrumentation list-explanations`.

Powered by github.com/grafana/otel-checker.

```
gcx instrumentation explain <id> [flags]
```

### Options

```
  -h, --help            help for explain
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, text, yaml (default "text")
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, CODEX_THREAD_ID, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx instrumentation](gcx_instrumentation.md)	 - Manage Grafana Instrumentation Hub

