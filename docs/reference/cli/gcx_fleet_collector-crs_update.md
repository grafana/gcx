## gcx fleet collector-crs update

[experimental] Replace a desired Collector custom resource.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

Replace a desired Collector custom resource.

The call fails when the id does not exist or the cluster does not. This is
a full replacement, not a patch: --namespace, --name, --release, and --spec
that are omitted are stored empty, and the previous values are not kept.
--cluster is required because the stored Collector CR must name a cluster.

The server assigns a new revision when namespace, name, release, or spec
changes. revision, appliedRevision, and applyError cannot be set here.

There is no get command. Use gcx fleet collector-crs list to read the
current fields before replacing them.

```
gcx fleet collector-crs update <id> [flags]
```

### Examples

```
  # Replace the spec and keep the other fields
  gcx fleet collector-crs update cr-a --cluster cluster-a --namespace alloy --name metrics --release k8smon --spec-file spec.yaml

  # Clear namespace, name, release, and spec
  gcx fleet collector-crs update cr-a --cluster cluster-a

  # Print the stored Collector CR, including the new revision
  gcx fleet collector-crs update cr-a --cluster cluster-a --name metrics -o json
```

### Options

```
      --cluster string     Cluster id. Required. The cluster must already exist
  -h, --help               help for update
      --jq string          jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string        Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --name string        Name of the custom resource. Omitted or empty is stored empty. Update replaces the Collector CR and does not keep the previous name
      --namespace string   Namespace of the custom resource. Omitted or empty is stored empty. Update replaces the Collector CR and does not keep the previous namespace
  -o, --output string      Output format. One of: agents, json, text, yaml (default "text")
      --release string     Helm release from the Kubernetes Monitoring chart. Omitted or empty is stored empty. Update replaces the Collector CR and does not keep the previous release
      --spec string        YAML spec, stored as written. Mutually exclusive with --spec-file. Omitted or empty is stored empty. Update replaces the Collector CR and does not keep the previous spec
      --spec-file string   File containing the YAML spec, or - for stdin. Mutually exclusive with --spec. The file contents are stored as written
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

