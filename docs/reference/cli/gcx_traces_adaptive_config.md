## gcx traces adaptive config

Manage the Adaptive Traces tenant configuration.

### Options

```
  -h, --help   help for config
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

* [gcx traces adaptive](gcx_traces_adaptive.md)	 - Manage Adaptive Traces resources
* [gcx traces adaptive config set](gcx_traces_adaptive_config_set.md)	 - Replace the Adaptive Traces tenant configuration.
* [gcx traces adaptive config show](gcx_traces_adaptive_config_show.md)	 - Show the Adaptive Traces tenant configuration.

