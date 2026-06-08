## gcx frontend apps

Manage Frontend Observability apps.

### Options

```
  -h, --help   help for apps
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

* [gcx frontend](gcx_frontend.md)	 - Manage Grafana Frontend Observability resources
* [gcx frontend apps apply-sourcemap](gcx_frontend_apps_apply-sourcemap.md)	 - Upload a sourcemap for a Frontend Observability app.
* [gcx frontend apps create](gcx_frontend_apps_create.md)	 - Create a Frontend Observability app from a file.
* [gcx frontend apps delete](gcx_frontend_apps_delete.md)	 - Delete a Frontend Observability app.
* [gcx frontend apps get](gcx_frontend_apps_get.md)	 - Get a Frontend Observability app by slug-id or name.
* [gcx frontend apps list](gcx_frontend_apps_list.md)	 - List Frontend Observability apps.
* [gcx frontend apps list-sessions](gcx_frontend_apps_list-sessions.md)	 - List sessions that have replay recordings.
* [gcx frontend apps play-session](gcx_frontend_apps_play-session.md)	 - Download and replay a session locally.
* [gcx frontend apps remove-sourcemap](gcx_frontend_apps_remove-sourcemap.md)	 - Remove sourcemap bundles from a Frontend Observability app.
* [gcx frontend apps show-segment](gcx_frontend_apps_show-segment.md)	 - Show events for a session recording segment.
* [gcx frontend apps show-session](gcx_frontend_apps_show-session.md)	 - Show recordings for a Frontend Observability session.
* [gcx frontend apps show-sourcemaps](gcx_frontend_apps_show-sourcemaps.md)	 - Show sourcemaps for a Frontend Observability app.
* [gcx frontend apps update](gcx_frontend_apps_update.md)	 - Update a Frontend Observability app from a file.

