## gcx fleet clusters list

[experimental] List clusters registered with Fleet Management.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

List clusters stored by the Fleet Management remote manager.

The API returns the full set. --limit only truncates what is printed.
--limit 0 prints every cluster.

Not for gcx instrumentation clusters, and not for gcx fleet collectors.

```
gcx fleet clusters list [flags]
```

### Examples

```
  # List a bounded summary
  gcx fleet clusters list

  # Print every cluster
  gcx fleet clusters list --limit 0

  # Select the identity fields
  gcx fleet clusters list --json id,name,namespace
```

### Options

```
  -h, --help            help for list
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int       Maximum number of clusters to return. 0 means all results are returned (default 50)
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

* [gcx fleet clusters](gcx_fleet_clusters.md)	 - [experimental] Manage clusters registered with Fleet Management.

