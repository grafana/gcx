## gcx fleet collector-crs create

[experimental] Create a desired Collector custom resource.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

Create a desired Collector custom resource.

--id and --cluster are required. The call fails when the id already exists
or the cluster does not. --namespace, --name, --release, and --spec are
stored empty when omitted.

The server assigns revision from namespace, name, release, and spec.
revision, appliedRevision, and applyError cannot be set here.

--spec and --spec-file are mutually exclusive. --spec-file - reads stdin.
The spec is stored as written, including a trailing newline.

```
gcx fleet collector-crs create [flags]
```

### Examples

```
  # Create a Collector CR with an inline spec
  gcx fleet collector-crs create --id cr-a --cluster cluster-a --namespace alloy --name metrics --release k8smon --spec "spec: {}"

  # Read the spec from a file
  gcx fleet collector-crs create --id cr-a --cluster cluster-a --spec-file spec.yaml

  # Print the stored Collector CR, including the server revision
  gcx fleet collector-crs create --id cr-a --cluster cluster-a --spec "spec: {}" -o json
```

### Options

```
      --cluster string     Cluster id. Required. The cluster must already exist
  -h, --help               help for create
      --id string          Collector CR id. Required. Chosen by the caller; fails when the id already exists
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

