## gcx fleet collector-crs list

[experimental] List desired Collector custom resources.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

List desired Collector custom resources.

Omit --cluster to list every Collector CR for the tenant. Set --cluster to
ask the server for that cluster only. An empty --cluster is the same as
omitting it.

The API returns the full matching set. --limit only truncates what is
printed. --limit 0 prints every match. Specs can be large; select fields
when you do not need spec.

appliedRevision is empty until the operator reports a revision. applyError
is empty when that revision was applied.

```
gcx fleet collector-crs list [flags]
```

### Examples

```
  # List a bounded summary of every Collector CR
  gcx fleet collector-crs list

  # List Collector CRs for one cluster
  gcx fleet collector-crs list --cluster cluster-a

  # Drop spec from the payload
  gcx fleet collector-crs list --cluster cluster-a --json id,clusterId,namespace,name,release,revision,appliedRevision,applyError

  # Print every Collector CR for one cluster
  gcx fleet collector-crs list --cluster cluster-a --limit 0
```

### Options

```
      --cluster string   Cluster id. Empty lists every Collector CR. Sent to the server as clusterId
  -h, --help             help for list
      --jq string        jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string      Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int        Maximum number of collector CRs to return. 0 means all results are returned (default 50)
  -o, --output string    Output format. One of: agents, json, table, wide, yaml (default "table")
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

* [gcx fleet collector-crs](gcx_fleet_collector-crs.md)	 - [experimental] Manage desired Collector custom resources.

