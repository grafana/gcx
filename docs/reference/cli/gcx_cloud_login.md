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
OAuth-issued token as the cloud entry's token. Some commands that talk to
grafana.com do not yet work with an OAuth token. For full functionality, pass
a Cloud Access Policy token via --cloud-token instead.

For non-interactive use (CI/CD, scripts), pass a Cloud Access Policy token
directly via --cloud-token.

Two endpoints can be configured independently, both defaulting to
https://grafana.com: --oauth-url is used only for the login flow here, while
--api-url is used by every command that talks to the Grafana Cloud API.

```
gcx cloud login [flags]
```

### Examples

```
  gcx cloud login
  gcx cloud login --cloud-token glsa_abc123
```

### Options

```
      --api-url string       Base URL for Grafana Cloud API resource calls (stacks etc.) (default "https://grafana.com")
      --cloud-token string   Cloud Access Policy token (skips interactive OAuth flow)
      --config string        Path to the configuration file to use
      --context string       Name of the context to use
  -h, --help                 help for login
      --oauth-url string     Base URL for the OAuth login flow (used only by this command) (default "https://grafana.com")
      --scope strings        OAuth2 scopes to request (default [stacks:read,stacks:write,stacks:delete,metrics:write,logs:write,traces:write,fleet-management:read,fleet-management:write])
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx cloud](gcx_cloud.md)	 - Manage your Grafana Cloud resources

