## grafanactl incidents

Manage Grafana IRM Incident resources.

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for incidents
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
* [grafanactl incidents activity](grafanactl_incidents_activity.md)	 - Manage incident activity timeline.
* [grafanactl incidents close](grafanactl_incidents_close.md)	 - Close (resolve) an incident.
* [grafanactl incidents create](grafanactl_incidents_create.md)	 - Create a new incident from a file.
* [grafanactl incidents get](grafanactl_incidents_get.md)	 - Get a single incident by ID.
* [grafanactl incidents list](grafanactl_incidents_list.md)	 - List incidents.
* [grafanactl incidents open](grafanactl_incidents_open.md)	 - Open an incident in the browser.
* [grafanactl incidents severities](grafanactl_incidents_severities.md)	 - Manage incident severity levels.

