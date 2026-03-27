## gcx dev

Manage Grafana resources as code

### Synopsis

Tools for managing Grafana resources as code: scaffold new projects, import existing resources from Grafana, generate typed Go stubs for new resources, lint resources, and serve resources locally.

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for dev
```

### Options inherited from parent commands

```
      --agent           Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --no-color        Disable color output
      --no-truncate     Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count   Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx dev generate](gcx_dev_generate.md)	 - Generate typed Go stubs for Grafana resources
* [gcx dev import](gcx_dev_import.md)	 - Import resources from Grafana and convert them to Go builder code
* [gcx dev lint](gcx_dev_lint.md)	 - Lint Grafana resources
* [gcx dev scaffold](gcx_dev_scaffold.md)	 - Scaffold a new Grafana resources-as-code project
* [gcx dev serve](gcx_dev_serve.md)	 - Serve Grafana resources locally

