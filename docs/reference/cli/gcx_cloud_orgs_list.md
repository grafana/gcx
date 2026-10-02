## gcx cloud orgs list

List the signed-in user's Grafana Cloud organisation memberships.

### Synopsis

List all organisation memberships returned by the Grafana Cloud OAuth API.
Requires a browser Cloud login with the profile scope. Cloud access-policy tokens
cannot enumerate user memberships. Existing logins may need re-authentication:
  gcx cloud login --scope profile

Returns organisation slugs and membership roles, not names or numeric IDs.
Membership does not guarantee permission to create stacks. To list stacks within
an organisation, use gcx cloud stacks list --org <slug>.

```
gcx cloud orgs list [flags]
```

### Examples

```
  gcx cloud orgs list
  gcx cloud orgs list -o json
  gcx cloud orgs list --json slug,role
```

### Options

```
  -h, --help            help for list
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, table, yaml (default "table")
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx cloud orgs](gcx_cloud_orgs.md)	 - Discover your Grafana Cloud organisations

