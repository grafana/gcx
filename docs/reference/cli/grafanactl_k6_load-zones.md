## grafanactl k6 load-zones

Manage K6 private load zones.

### Options

```
  -h, --help   help for load-zones
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GRAFANACTL_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [grafanactl k6](grafanactl_k6.md)	 - Manage K6 Cloud resources (projects, tests, env vars, schedules, load zones).
* [grafanactl k6 load-zones allowed-load-zones](grafanactl_k6_load-zones_allowed-load-zones.md)	 - Manage load zones allowed for a project.
* [grafanactl k6 load-zones allowed-projects](grafanactl_k6_load-zones_allowed-projects.md)	 - Manage projects allowed to use a load zone.
* [grafanactl k6 load-zones create](grafanactl_k6_load-zones_create.md)	 - Register a Private Load Zone.
* [grafanactl k6 load-zones delete](grafanactl_k6_load-zones_delete.md)	 - Deregister a Private Load Zone.
* [grafanactl k6 load-zones list](grafanactl_k6_load-zones_list.md)	 - List all K6 load zones.

