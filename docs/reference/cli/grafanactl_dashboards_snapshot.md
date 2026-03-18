## grafanactl dashboards snapshot

Render dashboard snapshots as PNG images

### Synopsis

Render one or more Grafana dashboards or individual panels as PNG images using the Grafana Image Renderer.

```
grafanactl dashboards snapshot <uid> [uid...] [flags]
```

### Examples

```

  # Snapshot a full dashboard
  grafanactl dashboards snapshot my-dashboard-uid

  # Snapshot a specific panel
  grafanactl dashboards snapshot my-dashboard-uid --panel 42

  # Snapshot with custom dimensions and time range
  grafanactl dashboards snapshot my-dashboard-uid --width 1000 --height 500 --theme light --from now-1h --to now

  # Snapshot using a time window shorthand
  grafanactl dashboards snapshot my-dashboard-uid --window 6h

  # Snapshot multiple dashboards to a specific directory
  grafanactl dashboards snapshot uid1 uid2 uid3 --output-dir ./snapshots

  # Snapshot with dashboard template variable overrides
  grafanactl dashboards snapshot my-dashboard-uid --var cluster=prod --var datasource=prometheus
```

### Options

```
      --concurrency int      Maximum number of concurrent render requests (default 10)
      --from string          Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
      --height int           Height of the rendered image in pixels (default: -1/full-page for dashboard, 600 for panel)
  -h, --help                 help for snapshot
      --org-id int           Grafana organization ID (default 1)
      --output-dir string    Directory to write PNG files to (created if it does not exist) (default ".")
      --panel int            Panel ID to render a single panel instead of the full dashboard
      --theme string         Grafana theme (light or dark) (default "dark")
      --to string            End time (RFC3339, Unix timestamp, or relative like 'now')
      --tz string            Timezone (e.g. 'UTC', 'America/New_York')
      --var stringToString   Dashboard template variable overrides (e.g. --var cluster=prod --var datasource=prometheus) (default [])
      --width int            Width of the rendered image in pixels (default: 1920 for dashboard, 800 for panel)
      --window string        Time window shorthand (e.g. '1h', '7d'); expands to --from now-{window} --to now; mutually exclusive with --from/--to
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

* [grafanactl dashboards](grafanactl_dashboards.md)	 - Manage Grafana dashboards

