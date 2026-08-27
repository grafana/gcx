## gcx frontend sessions get

Write Frontend Observability session telemetry to a text file.

### Synopsis

Fetch one Frontend Observability session and write it as plain text.

Two labeled blocks are produced: session metadata (once) and events (the user
journey). Without --save, Pinot metadata prints as tables and the journey as
TSV; Loki metadata is named fields once (sdk, app, user, os, geo, browser,
device, session_id, session_attr), and events are timestamp then the log line
with those envelope keys stripped. --save writes Pinot TSV or the Loki stream.
There is no JSON or YAML encoding of the dump.

Use --save so agents receive a small artifact receipt on stdout and then read
the file.

--datasource selects the backend (loki or pinot), not a Grafana UID. The UID
is resolved from config or auto-discovery.

Faro apps do not store web vs mobile on the app resource. Omit --app-type and
gcx infers it from sdkName / osName on the session (so mobile journeys exclude
app_memory / app_cpu_usage). Pass --app-type to override.

```
gcx frontend sessions get <session-id> [flags]
```

### Examples

```
  # Pinot on stdout (metadata tables, journey TSV)
  gcx frontend sessions get 7TiMbCCvby --app 66 --datasource pinot --since 7d

  # Pinot dump to a file; app type inferred from telemetry
  gcx frontend sessions get 7TiMbCCvby --app 66 --datasource pinot --since 7d \
    --save /tmp/session-7TiMbCCvby.txt

  # Loki dump
  gcx frontend sessions get 7TiMbCCvby --app 66 --datasource loki --since 7d \
    --save /tmp/session-7TiMbCCvby.txt

  # Force mobile SQL (app_memory / app_cpu_usage excluded)
  gcx frontend sessions get kwwAkkXwas --app 96 --app-type mobile \
    --datasource pinot --since 7d --save /tmp/session-kwwAkkXwas.txt
```

### Options

```
      --app string          Frontend Observability app slug-id or numeric id (required)
      --app-type string     web or mobile. Optional: inferred from sdkName/osName when omitted
      --datasource string   Telemetry backend: loki or pinot (default loki) (default "loki")
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for get
      --save string         Write the session dump to this path instead of stdout
      --since string        Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
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

* [gcx frontend sessions](gcx_frontend_sessions.md)	 - Inspect Frontend Observability sessions.

