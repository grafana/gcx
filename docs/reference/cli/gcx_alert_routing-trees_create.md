## gcx alert routing-trees create

Create a named routing tree from a manifest.

### Synopsis

Create a routing tree from a native RoutingTree manifest. Create never
updates an existing tree; an existing or reserved name ("user-defined",
"default") fails with the server's conflict error.

```
gcx alert routing-trees create -f <file> [flags]
```

### Examples

```
  gcx alert routing-trees create -f team-a.yaml
```

### Options

```
      --api-version string   Must match the manifest's apiVersion when set; the manifest decides the version
  -f, --filename string      Path to a JSON/YAML RoutingTree manifest ('-' reads from stdin)
  -h, --help                 help for create
      --jq string            jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string        Output format. One of: agents, json, text, yaml (default "text")
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from known agent identity variables. Set GCX_AGENT_NAME to identify a supported harness, or GCX_AGENT_MODE to control the mode.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx alert routing-trees](gcx_alert_routing-trees.md)	 - Manage notification routing trees (default and named).

