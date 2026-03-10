## grafanactl dev

Manage Grafana resources as code

### Synopsis

Tools for managing Grafana resources as code: scaffold new projects, import existing resources from Grafana, and generate typed Go stubs for new resources.

### Options

```
  -h, --help   help for dev
```

### Options inherited from parent commands

```
      --agent           Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GRAFANACTL_AGENT_MODE env vars.
      --no-color        Disable color output
      --no-truncate     Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count   Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [grafanactl](grafanactl.md)	 - 
* [grafanactl dev generate](grafanactl_dev_generate.md)	 - Generate typed Go stubs for Grafana resources
* [grafanactl dev import](grafanactl_dev_import.md)	 - Import resources from Grafana and convert them to Go builder code
* [grafanactl dev scaffold](grafanactl_dev_scaffold.md)	 - Scaffold a new Grafana resources-as-code project

