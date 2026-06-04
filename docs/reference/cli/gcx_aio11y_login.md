## gcx aio11y login

Provision sigil credentials for coding-agent plugins from the current context.

### Synopsis

Provision the shared sigil credentials file (~/.config/sigil/config.env) used by
the Grafana AI Observability coding-agent plugins (Cursor, Claude Code, Codex,
Copilot, OpenCode, pi).

Endpoints and the tenant ID are resolved from the current gcx Grafana Cloud
context:
  - SIGIL_ENDPOINT                    the stack's GCOM regionSigilUrl
  - SIGIL_AUTH_TENANT_ID              the Grafana Cloud stack instance ID
  - SIGIL_OTEL_EXPORTER_OTLP_ENDPOINT the GCOM OTLP gateway endpoint

By default gcx mints a dedicated Cloud Access Policy token scoped to
`sigil:write`, `metrics:write`, `traces:write` and writes it as
SIGIL_AUTH_TOKEN. This requires the context's cloud token to have
`accesspolicies:write`. If it doesn't, gcx falls back to writing the
context token directly and tells you how to create a scoped one.

Re-running reuses the access policy and rotates the token. Existing optional
keys in config.env (e.g. SIGIL_TAGS) are preserved.

```
gcx aio11y login [flags]
```

### Examples

```
  gcx aio11y login
  gcx aio11y login --content-capture-mode full
  gcx aio11y login --no-provision          # write the context token as-is
  gcx aio11y login --token glc_xxx         # use a token you created yourself
  gcx aio11y login --dry-run
```

### Options

```
      --config-path string            Override the sigil config.env path (default: $XDG_CONFIG_HOME/sigil/config.env)
      --content-capture-mode string   Set SIGIL_CONTENT_CAPTURE_MODE. One of: metadata_only, no_tool_content, full, full_with_metadata_spans
      --dry-run                       Resolve and print the configuration without minting a token or writing config.env
  -h, --help                          help for login
      --json string                   Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --no-provision                  Don't mint a dedicated token; write the context's Cloud Access Policy token instead
      --otlp-endpoint string          Override the OTLP gateway endpoint (SIGIL_OTEL_EXPORTER_OTLP_ENDPOINT) instead of reading it from GCOM
  -o, --output string                 Output format. One of: agents, json, text, yaml (default "text")
      --sigil-endpoint string         Override the Sigil API endpoint (SIGIL_ENDPOINT) instead of reading it from GCOM
      --token string                  Use this token as SIGIL_AUTH_TOKEN verbatim (skips automatic provisioning)
      --token-expiry string           Expiry for the provisioned token (RFC3339, e.g. 2027-01-01T00:00:00Z); default: no expiry
      --token-name string             Name for the provisioned token (default: sigil-<stack-slug>)
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

* [gcx aio11y](gcx_aio11y.md)	 - Manage Grafana AI Observability resources

