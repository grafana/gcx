## gcx k6 runs traces

[experimental] Inspect browser traces for k6 test runs.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.

Search and get browser traces that belong to k6 test runs.

### Options

```
  -h, --help   help for traces
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

* [gcx k6 runs](gcx_k6_runs.md)	 - Manage k6 test runs.
* [gcx k6 runs traces get](gcx_k6_runs_traces_get.md)	 - [experimental] Get a browser trace for a k6 test run.
* [gcx k6 runs traces list](gcx_k6_runs_traces_list.md)	 - [experimental] List browser traces for a k6 test run.

