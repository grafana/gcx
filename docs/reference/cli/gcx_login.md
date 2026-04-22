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

```
gcx login [CONTEXT_NAME] [flags]
```

### Examples

```
  gcx login
  gcx login prod
  gcx login prod --server https://prod.grafana.net
  gcx login --yes prod --token glsa_xxx
  gcx login --yes --server https://localhost:3000 --token glsa_xxx
```

### Options

```
      --cloud                  Force Grafana Cloud target (skip auto-detection)
      --cloud-api-url string   Override Grafana Cloud API URL
      --cloud-token string     Grafana Cloud API token (enables Cloud management features)
      --config string          Path to the configuration file to use
      --context string         Name of the context to use
  -h, --help                   help for login
      --server string          Grafana server URL (e.g. https://my-stack.grafana.net)
      --token string           Grafana service account token
      --yes                    Non-interactive: skip optional prompts and use defaults
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations

