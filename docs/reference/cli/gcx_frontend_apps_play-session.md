## gcx frontend apps play-session

Download and replay a session locally.

### Synopsis

Downloads all segments for a session and serves a local rrweb-player. Provides a control API for agent-driven playback via WebSocket.

```
gcx frontend apps play-session <app-name> <session-id> [flags]
```

### Examples

```
  # Play a session (prints local URL).
  gcx frontend apps play-session my-web-app-42 abc-session-123

  # Open browser automatically.
  gcx frontend apps play-session my-web-app-42 abc-session-123 --open

  # Use a specific port.
  gcx frontend apps play-session my-web-app-42 abc-session-123 --port 8080
```

### Options

```
  -h, --help                    help for play-session
      --open                    Open a browser automatically
      --player-version string   rrweb-player version to load from CDN (default "2.0.0-alpha.17")
      --port int                Port to serve on (0 = random available port)
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

* [gcx frontend apps](gcx_frontend_apps.md)	 - Manage Frontend Observability apps.

