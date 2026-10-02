## gcx fleet clusters create

[experimental] Create a Fleet Management cluster.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

Create a cluster in the Fleet Management remote manager.

--id is required and is chosen by the caller. The call fails when that id
already exists. --name and --namespace are stored empty when omitted.

The in-cluster operator can also introduce a cluster by connecting. This
command is the direct way to create the record first.

```
gcx fleet clusters create [flags]
```

### Examples

```
  # Create a cluster with a name and operator namespace
  gcx fleet clusters create --id cluster-a --name prod --namespace alloy

  # Create a cluster with only an id
  gcx fleet clusters create --id cluster-b

  # Print the stored cluster as JSON
  gcx fleet clusters create --id cluster-a --name prod -o json
```

### Options

```
  -h, --help               help for create
      --id string          Cluster id. Required. Chosen by the caller; fails when the id already exists
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

