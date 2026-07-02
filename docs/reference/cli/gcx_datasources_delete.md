## gcx datasources delete

Delete one or more datasources

### Synopsis

Delete one or more datasources by UID.

Deletion prompts for confirmation unless --force/--yes, GCX_AUTO_APPROVE, or
agent mode is in effect.

Exit codes: 0 (all deleted), 4 (some deletions failed).

```
gcx datasources delete UID... [flags]
```

### Examples

```

	# Delete one datasource (prompts to confirm)
	gcx datasources delete sentry-dev

	# Delete several without prompting
	gcx datasources delete sentry-dev sentry-staging --yes

	# Preview only
	gcx datasources delete sentry-dev --dry-run
```

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --dry-run          Report what would be deleted without deleting
      --force            Skip the confirmation prompt
  -h, --help             help for delete
      --jq string        jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string      Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string    Output format. One of: agents, json, text, yaml (default "text")
  -y, --yes              Skip the confirmation prompt
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

* [gcx datasources](gcx_datasources.md)	 - Manage and query Grafana datasources

