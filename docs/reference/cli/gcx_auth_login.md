## gcx auth login

Authenticate to a Grafana stack with OAuth

### Synopsis

Opens a browser to authenticate with your Grafana stack using OAuth. This is an
alternative to using an access token.

On success, the CLI token and proxy endpoint are saved to your current config
context. Subsequent commands will use the proxy to access Grafana's API with
your identity and RBAC permissions.

Your current context must be set to a context that has a grafana server
configured before you can call this command. For example:
	gcx config set contexts.<context>.grafana.server https://your-stack.grafana.net
	gcx config use-context <context>

```
gcx auth login [flags]
```

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for login
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

