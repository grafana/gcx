## gcx login

Log in to a Grafana instance

### Synopsis

Authenticate to a Grafana instance (Cloud or on-premises) and save the
credentials to the selected config context.

Pass CONTEXT_NAME to target a specific context:
  - If the context exists, re-authenticate it (server and other fields preserved).
  - If it does not exist, create a new context with that name.

Without CONTEXT_NAME, re-authenticates the current context, or starts a
first-time setup if no current context is configured.

--server takes the URL of a Grafana stack, such as https://my-stack.grafana.net.
It does not take the Grafana Cloud portal at grafana.com, which manages stacks
but serves no Grafana instance API.

Grafana instance authentication (choose one, for non-interactive use):
  --oauth        Browser-based OAuth (recommended for Grafana Cloud). Opens a browser for the user to approve; works in agent mode.
  --token        Grafana service-account token (created inside the Grafana instance).
                 See: https://grafana.com/docs/grafana/latest/administration/service-accounts.md

Grafana Cloud platform credential (optional, and in addition to the above):
  --cloud-token  Grafana Cloud access-policy token (created at grafana.com).
                 It authenticates the Cloud product commands: sm, k6, irm, slo, and fleet.
                 It cannot authenticate the Grafana instance, so pass --oauth or --token as well.
                 See: https://grafana.com/docs/grafana-cloud/security-and-account-management/authentication-and-permissions/access-policies/create-access-policies.md

```
gcx login [CONTEXT_NAME] [flags]
```

### Examples

```
  gcx login
  gcx login prod
  gcx login prod --server https://prod.grafana.net
  gcx login prod --server https://prod.grafana.net --oauth
  gcx login --yes prod --token glsa_xxx
  gcx login --yes prod --server https://prod.grafana.net --token glsa_xxx --cloud-token glc_xxx
  gcx login --yes --server https://localhost:3000 --token glsa_xxx
```

### Options

```
      --allow-server-override     Allow re-pointing an existing context at a different server URL
      --cloud                     Force Grafana Cloud target (skip auto-detection)
      --cloud-api-url string      Override Grafana Cloud API URL
      --cloud-token string        Grafana Cloud access-policy token for the Cloud product commands (does not authenticate the Grafana instance)
      --config string             Path to the configuration file to use
      --context string            Name of the context to use
  -h, --help                      help for login
      --jq string                 jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string               Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --oauth                     Authenticate via browser-based OAuth (recommended for Grafana Cloud). Works non-interactively and in agent mode: opens a browser for the user to approve.
      --oauth-callback-port int   Fixed local port for the OAuth callback server (default: auto-pick from 54321-54399). Useful when only specific ports are forwarded between a remote host and your browser
      --org-id int                Grafana organization ID (defaults to 1 for on-prem)
  -o, --output string             Output format. One of: agents, json, text, yaml (default "text")
      --server string             Grafana stack URL (e.g. https://my-stack.grafana.net), not the grafana.com portal
      --token string              Grafana service account token
      --yes                       Non-interactive: skip optional prompts and use defaults
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

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations

