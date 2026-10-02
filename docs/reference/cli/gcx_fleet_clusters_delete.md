## gcx fleet clusters delete

[experimental] Delete a Fleet Management cluster.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

Delete a cluster from the Fleet Management remote manager.

The call fails when the id does not exist, and when the cluster still has
Collector custom resources. Delete those first:

  gcx fleet collector-crs list --cluster <id>
  gcx fleet collector-crs delete <collector-cr-id>

```
gcx fleet clusters delete <id> [flags]
```

### Examples

```
  # Delete a cluster that has no Collector CRs
  gcx fleet clusters delete cluster-a

  # Skip the confirmation prompt
  gcx fleet clusters delete cluster-a --force

  # Print the deletion receipt as JSON
  gcx fleet clusters delete cluster-a --force -o json
```

### Options

```
      --force           Skip confirmation prompt
  -h, --help            help for delete
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, text, yaml (default "text")
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

