## gcx fleet clusters

[experimental] Manage clusters registered with Fleet Management.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

Manage clusters stored by the Fleet Management remote manager.

A cluster is the record an in-cluster operator belongs to. It has an id, a
name, and the namespace the operator runs in. Use this to list those records
or to create one before adding Collector custom resources.

Not for Kubernetes Monitoring feature flags (cost metrics, Beyla, node logs).
Those are gcx instrumentation clusters.

Not for collectors that have registered with Fleet Management. Those are
gcx fleet collectors.

The operator connection (ClusterConnection) is not a command. The operator
opens that channel itself.

### Examples

```
  # List clusters
  gcx fleet clusters list

  # Create a cluster, then add a Collector CR for it
  gcx fleet clusters create --id cluster-a --name prod --namespace alloy
  gcx fleet collector-crs create --id cr-a --cluster cluster-a --name metrics --namespace alloy
```

### Options

```
  -h, --help   help for clusters
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
* [gcx fleet clusters create](gcx_fleet_clusters_create.md)	 - [experimental] Create a Fleet Management cluster.
* [gcx fleet clusters delete](gcx_fleet_clusters_delete.md)	 - [experimental] Delete a Fleet Management cluster.
* [gcx fleet clusters list](gcx_fleet_clusters_list.md)	 - [experimental] List clusters registered with Fleet Management.
* [gcx fleet clusters update](gcx_fleet_clusters_update.md)	 - [experimental] Replace a Fleet Management cluster.

