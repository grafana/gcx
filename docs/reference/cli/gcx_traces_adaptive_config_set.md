## gcx traces adaptive config set

Replace the Adaptive Traces tenant configuration.

### Synopsis

Replace the Adaptive Traces tenant configuration with the contents of the supplied file. The API does not support partial patches — the entire payload is required and the existing document is overwritten.

To avoid clobbering fields you did not intend to change, run `gcx traces adaptive config show` first, edit the returned document, and pass the full result back to `set`. Any field omitted from the payload is dropped, not preserved.

```
gcx traces adaptive config set [flags]
```

### Options

```
  -f, --filename string   File containing the full configuration payload (use - for stdin)
      --force             Skip confirmation prompt
  -h, --help              help for set
      --json string       Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string     Output format. One of: agents, json, yaml (default "yaml")
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

* [gcx traces adaptive config](gcx_traces_adaptive_config.md)	 - Manage the Adaptive Traces tenant configuration.

