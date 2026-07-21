## gcx k6 load-zones update-allowed-projects

Replace the set of projects allowed to use a load zone.

```
gcx k6 load-zones update-allowed-projects <load-zone-id> [flags]
```

### Options

```
  -f, --filename string   File containing the complete replacement set of project IDs (JSON array)
  -h, --help              help for update-allowed-projects
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

* [gcx k6 load-zones](gcx_k6_load-zones.md)	 - Manage k6 private load zones.

