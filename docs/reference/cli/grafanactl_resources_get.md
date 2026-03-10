## grafanactl resources get

Get resources from Grafana

### Synopsis

Get resources from Grafana using a specific format. See examples below for more details.

```
grafanactl resources get [RESOURCE_SELECTOR]... [flags]
```

### Examples

```

	# Everything:

	grafanactl resources get dashboards/foo

	# All instances for a given kind(s):

	grafanactl resources get dashboards
	grafanactl resources get dashboards folders

	# Single resource kind, one or more resource instances:

	grafanactl resources get dashboards/foo
	grafanactl resources get dashboards/foo,bar

	# Single resource kind, long kind format:

	grafanactl resources get dashboard.dashboards/foo
	grafanactl resources get dashboard.dashboards/foo,bar

	# Single resource kind, long kind format with version:

	grafanactl resources get dashboards.v1alpha1.dashboard.grafana.app/foo
	grafanactl resources get dashboards.v1alpha1.dashboard.grafana.app/foo,bar

	# Multiple resource kinds, one or more resource instances:

	grafanactl resources get dashboards/foo folders/qux
	grafanactl resources get dashboards/foo,bar folders/qux,quux

	# Multiple resource kinds, long kind format:

	grafanactl resources get dashboard.dashboards/foo folder.folders/qux
	grafanactl resources get dashboard.dashboards/foo,bar folder.folders/qux,quux

	# Multiple resource kinds, long kind format with version:

	grafanactl resources get dashboards.v1alpha1.dashboard.grafana.app/foo folders.v1alpha1.folder.grafana.app/qux
```

### Options

```
  -h, --help              help for get
      --json string       Comma-separated list of fields to include in JSON output, or '?' to discover available fields
      --on-error string   How to handle errors during resource operations:
                            ignore — continue processing all resources and exit 0
                            fail   — continue processing all resources and exit 1 if any failed (default)
                            abort  — stop on the first error and exit 1 (default "fail")
  -o, --output string     Output format. One of: json, text, wide, yaml (default "text")
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

* [grafanactl resources](grafanactl_resources.md)	 - Manipulate Grafana resources

