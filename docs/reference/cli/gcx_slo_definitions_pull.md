## gcx slo definitions pull

Pull resources to disk (Deprecated: use gcx resources pull).

### Synopsis

Deprecated: use gcx resources pull slos.v1alpha1.slo.ext.grafana.app -p PATH -o yaml instead.
This compatibility command retains its Kind/name.yaml layout.

```
gcx slo definitions pull [flags]
```

### Options

```
  -h, --help                help for pull
  -d, --output-dir string   Directory to write resources to (default ".")
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx slo definitions](gcx_slo_definitions.md)	 - Manage SLO definitions.

