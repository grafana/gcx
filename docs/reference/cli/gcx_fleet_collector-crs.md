## gcx fleet collector-crs

[experimental] Manage desired Collector custom resources.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

Manage desired Collector custom resources stored by the Fleet Management remote manager.

A Collector CR is the custom resource an in-cluster operator should apply.
It belongs to one cluster. The next time that cluster's operator connects,
the server includes the current desired set.

Not for collectors that have registered with Fleet Management. Those are
agents that have phoned home: gcx fleet collectors.

Not for pipeline configuration assigned to registered collectors. That is
gcx fleet pipelines.

List shows the revision the server assigned and the revision the operator
last reported as applied. Create and update ignore revision, appliedRevision,
and applyError. The operator connection (ClusterConnection) is not a command.

### Examples

```
  # List every desired Collector CR
  gcx fleet collector-crs list

  # List Collector CRs for one cluster
  gcx fleet collector-crs list --cluster cluster-a

  # Create one, with the spec read from a file
  gcx fleet collector-crs create --id cr-a --cluster cluster-a --namespace alloy --name metrics --release k8smon --spec-file spec.yaml
```

### Options

```
  -h, --help   help for collector-crs
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

* [gcx fleet](gcx_fleet.md)	 - Manage Grafana Fleet Management pipelines, collectors, clusters, and collector CRs
* [gcx fleet collector-crs create](gcx_fleet_collector-crs_create.md)	 - [experimental] Create a desired Collector custom resource.
* [gcx fleet collector-crs delete](gcx_fleet_collector-crs_delete.md)	 - [experimental] Delete a desired Collector custom resource.
* [gcx fleet collector-crs list](gcx_fleet_collector-crs_list.md)	 - [experimental] List desired Collector custom resources.
* [gcx fleet collector-crs update](gcx_fleet_collector-crs_update.md)	 - [experimental] Replace a desired Collector custom resource.

