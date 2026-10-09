## gcx frontend sessions

Inspect Frontend Observability sessions.

### Options

```
  -h, --help   help for sessions
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from known agent identity variables. Set GCX_AGENT_NAME to identify a supported harness, or GCX_AGENT_MODE to control the mode.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx frontend](gcx_frontend.md)	 - Manage Grafana Frontend Observability resources
* [gcx frontend sessions get](gcx_frontend_sessions_get.md)	 - Write Frontend Observability session telemetry to a text file.
* [gcx frontend sessions get-replay](gcx_frontend_sessions_get-replay.md)	 - Save all replays for a Frontend Observability session.

