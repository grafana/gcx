## gcx frontend sessions get-replay

Save all replays for a Frontend Observability session.

### Synopsis

Save every recording in a session to one JSON file. Each recording
contains its complete rrweb event stream, assembled from its segments in order.
Recording boundaries are preserved because separate recordings can overlap in time.

```
gcx frontend sessions get-replay <session-id> [flags]
```

### Examples

```
  # Save the replay for a session to a private JSON file.
  gcx frontend sessions get-replay abc-session-123 --app my-web-app-42 --save replay.json
```

### Options

```
      --app string    Frontend Observability app slug-id or numeric id (required)
  -h, --help          help for get-replay
      --save string   Path for the complete session replay JSON (required)
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx frontend sessions](gcx_frontend_sessions.md)	 - Inspect Frontend Observability sessions.

