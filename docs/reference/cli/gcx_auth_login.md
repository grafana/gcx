## gcx auth login

Authenticate to a Grafana stack with OAuth

### Synopsis

Opens a browser to authenticate with your Grafana stack using OAuth. This is an
alternative to using an access token.

On success, the CLI token and proxy endpoint are saved to the selected config
context. Subsequent commands will use the proxy to access Grafana's API with
your identity and RBAC permissions.

If --server is provided, gcx uses that server for this login and saves it to
the selected context. This lets you bootstrap auth without preconfiguring
grafana.server.

Without --server, the selected context must already define grafana.server. For
example:
	gcx config set contexts.<context>.grafana.server https://your-stack.grafana.net
	gcx config use-context <context>

```
gcx auth login [flags]
```

### Examples

```
  gcx auth login --server https://your-stack.grafana.net
  gcx auth login --context prod --server https://prod.grafana.net
  gcx auth login
```

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for login
      --server string    Grafana server URL to use for this login and save to the selected context
```

### Options inherited from parent commands

```
      --agent           Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --no-color        Disable color output
      --no-truncate     Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count   Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx auth](gcx_auth.md)	 - Manage authentication

