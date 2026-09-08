## gcx k6 runs artifacts

[experimental] List and download browser artifacts for k6 test runs.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.

List and download browser screenshots that belong to k6 test runs.

### Options

```
  -h, --help   help for artifacts
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
* [gcx k6 runs artifacts download](gcx_k6_runs_artifacts_download.md)	 - [experimental] Download browser artifacts for a k6 test run.
* [gcx k6 runs artifacts list](gcx_k6_runs_artifacts_list.md)	 - [experimental] List browser artifacts for a k6 test run.

