## gcx k6 runs artifacts download

[experimental] Download browser artifacts for a k6 test run.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.

Discover and download browser screenshots for one k6 test run. With no artifact paths, download all discovered artifacts. Existing files are not overwritten.

```
gcx k6 runs artifacts download <run-id> [artifact-path...] [flags]
```

### Examples

```
  gcx k6 runs artifacts download 12345
  gcx k6 runs artifacts download 12345 --output-dir ./screenshots
  gcx k6 runs artifacts download 12345 '12345/files/screenshots/screenshots/home.png'
```

### Options

```
  -h, --help                help for download
  -d, --output-dir string   Directory for downloaded artifacts (default k6-run-<run-id>-artifacts)
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

* [gcx k6 runs artifacts](gcx_k6_runs_artifacts.md)	 - [experimental] List and download browser artifacts for k6 test runs.

