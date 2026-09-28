## gcx experiments create

[experimental] Create an Odin experiment from a manifest.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.

Create one Experiment through the Odin app plugin on the selected Grafana instance.
Supply the full resource as YAML or JSON. The plugin chooses the namespace and
validates the resource. New experiments should use an existing Grafana feature
toggle and include analyticsConfig with a query, variant mapping, and metrics.
Use spec.status: draft until the experiment is ready. This command never updates
an existing experiment.

```
gcx experiments create [flags]
```

### Examples

```
  gcx experiments create --example -o yaml
  gcx experiments create -f experiment.yaml
  gcx experiments create -f - < experiment.json
  gcx experiments create -f experiment.yaml -o json
```

### Options

```
      --example           Print a complete example manifest without creating an experiment
  -f, --filename string   Complete Experiment YAML or JSON manifest (use - for stdin)
  -h, --help              help for create
      --jq string         jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string       Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string     Output format. One of: agents, json, yaml (default "yaml")
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

* [gcx experiments](gcx_experiments.md)	 - [experimental] Work with Odin experiments.

