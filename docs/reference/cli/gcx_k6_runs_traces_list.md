## gcx k6 runs traces list

[experimental] List browser traces for a k6 test run.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.

List browser iteration traces for one k6 test run. Use --query to supply a narrower TraceQL expression.

```
gcx k6 runs traces list <run-id> [flags]
```

### Examples

```
  gcx k6 runs traces list 12345
  gcx k6 runs traces list 12345 --query '{ span.test.scenario = "ui" && span.test.vu = 1 }' --limit 50 -o json
```

### Options

```
  -h, --help            help for list
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int       Maximum number of traces to return (default 500)
  -o, --output string   Output format. One of: agents, json, table, yaml (default "table")
      --query string    TraceQL expression for trace search (default "{ name = \"iteration\" && span.test.iteration.number >= 0 && span.test.vu >= 0 && span.test.scenario != \"\" }")
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

