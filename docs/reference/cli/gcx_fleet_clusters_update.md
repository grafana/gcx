## gcx fleet clusters update

[experimental] Replace a Fleet Management cluster.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

Replace a cluster in the Fleet Management remote manager.

The call fails when the id does not exist. This is a full replacement, not a
patch: --name and --namespace that are omitted are stored empty, and the
previous values are not kept. Pass every field that should remain.

There is no get command. Use gcx fleet clusters list to read the current
name and namespace before replacing them.

```
gcx fleet clusters update <id> [flags]
```

### Examples

```
  # Replace the name and keep the operator namespace
  gcx fleet clusters update cluster-a --name staging --namespace alloy

  # Clear the name and namespace
  gcx fleet clusters update cluster-a

  # Print the stored cluster as JSON
  gcx fleet clusters update cluster-a --name staging --namespace alloy -o json
```

### Options

```
  -h, --help               help for update
      --jq string          jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string        Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --name string        Cluster name. Omitted or empty is stored empty. Update replaces the cluster and does not keep the previous name
      --namespace string   Namespace the operator runs in. Omitted or empty is stored empty. Update replaces the cluster and does not keep the previous namespace
  -o, --output string      Output format. One of: agents, json, text, yaml (default "text")
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

