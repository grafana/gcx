## gcx slo reports pull

Pull SLO reports to disk (Deprecated: use gcx resources pull).

### Synopsis

Pull SLO reports to disk.

Deprecated: use gcx resources pull reports.v1alpha1.slo.ext.grafana.app -p PATH -o yaml instead.
This compatibility command retains its Kind/name.yaml layout.

```
gcx slo reports pull [flags]
```

### Options

```
  -h, --help                help for pull
  -d, --output-dir string   Directory to write SLO reports to (default ".")
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

* [gcx slo reports](gcx_slo_reports.md)	 - Manage SLO reports.

