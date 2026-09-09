## gcx cloud login

Authenticate with the Grafana Cloud API (GCOM)

### Synopsis

Authenticate with the Grafana Cloud API and store the token in the gcx config.

This is different from "gcx login", which authenticates to a specific
Grafana stack instance. "gcx cloud login" authenticates against the
Grafana Cloud platform API (grafana.com), enabling commands that manage
Cloud resources like stacks and access policies.

By default, opens a browser for interactive OAuth2 authentication.

EXPERIMENTAL: interactive OAuth login is an experimental flow that stores an
OAuth-issued token in the cloud entry's oauth-token field. Some commands that
talk to grafana.com do not yet work with an OAuth token, and the token cannot
be refreshed - when it expires, run this command again. For full
functionality, pass a Cloud Access Policy token via --cloud-token instead.

For non-interactive use (CI/CD, scripts), pass a Cloud Access Policy token
directly via --cloud-token.

The OAuth and API endpoints default to https://grafana.com. Supplying only one
of --oauth-url or --api-url selects that URL for both operations. Supplying
both preserves the explicit OAuth-origin/API-destination pair.

```
gcx cloud login [flags]
```

### Examples

```
  gcx cloud login
  gcx cloud login --cloud-token glc_abc123
```

### Options

```
      --api-url string       Base URL for Grafana Cloud API resource calls (stacks etc.) (default "https://grafana.com")
      --cloud-token string   Cloud Access Policy token (skips interactive OAuth flow)
      --config string        Path to the configuration file to use
      --context string       Name of the context to use
  -h, --help                 help for login
      --oauth-manual         Complete browser OAuth without a local callback server: gcx prints the URL, then reads the redirect URL that you copy from the browser address bar. Use this when gcx runs on a remote host and the browser runs on your own computer
      --oauth-url string     Base URL for the OAuth login flow (used only by this command) (default "https://grafana.com")
      --scope strings        OAuth2 scopes to request (default [stacks:read,stacks:write,stacks:delete,metrics:write,logs:write,traces:write])
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx cloud](gcx_cloud.md)	 - Manage your Grafana Cloud resources

