## gcx instrumentation clusters apps remove

Remove Beyla instrumentation for a namespace

### Synopsis

Remove Beyla auto-instrumentation for a namespace by removing its entry
from the cluster's app instrumentation configuration.

The namespace entry is removed from namespaces[] via a whole-list replacement
(SetAppInstrumentation). When no namespace entries remain with included content,
the backend deletes the app pipeline entirely.

This command requires --yes to proceed.

```
gcx instrumentation clusters apps remove <cluster> <namespace> [flags]
```

### Options

```
  -h, --help            help for remove
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, text, yaml (default "text")
      --yes             Confirm removal of namespace app instrumentation
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

* [gcx instrumentation clusters apps](gcx_instrumentation_clusters_apps.md)	 - Manage namespace-level Beyla instrumentation for a cluster

