## gcx irm oncall sync-plugin

Request a refresh of the IRM copy of the Grafana users and teams.

### Synopsis

Request a refresh of the IRM copy of the Grafana users and teams.

IRM mirrors the Grafana users and teams, and refreshes that copy on a
schedule. Until the refresh lands, an IRM object that references a new team or
a new user fails with "Object does not exist".

Run this command after you create a Grafana team or user, before you create
the IRM objects that reference it.

The backend accepts the request and refreshes in the background, so a
successful call does not prove that the copy is already current.

```
gcx irm oncall sync-plugin [flags]
```

### Options

```
  -h, --help            help for sync-plugin
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, text, yaml (default "text")
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

* [gcx irm oncall](gcx_irm_oncall.md)	 - Manage Grafana OnCall resources.

