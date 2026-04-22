## gcx irm incidents

Manage incidents.

### Options

```
  -h, --help   help for incidents
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --limit int          Maximum number of items to return from list operations (0 for all; defaults to 50 in agent mode)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx irm](gcx_irm.md)	 - Manage Grafana IRM (OnCall + Incidents)
* [gcx irm incidents activity](gcx_irm_incidents_activity.md)	 - Manage incident activity timeline.
* [gcx irm incidents close](gcx_irm_incidents_close.md)	 - Close (resolve) an incident.
* [gcx irm incidents create](gcx_irm_incidents_create.md)	 - Create a new incident from a file.
* [gcx irm incidents get](gcx_irm_incidents_get.md)	 - Get a single incident by ID.
* [gcx irm incidents list](gcx_irm_incidents_list.md)	 - List incidents.
* [gcx irm incidents open](gcx_irm_incidents_open.md)	 - Open an incident in the browser.
* [gcx irm incidents severities](gcx_irm_incidents_severities.md)	 - Manage incident severity levels.

