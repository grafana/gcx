## gcx resources get

Get resources from Grafana

### Synopsis

Get resources from Grafana using a specific format. See examples below for more details.

```
gcx resources get [RESOURCE_SELECTOR]... [flags]
```

### Examples

```

	# Everything:

	gcx resources get dashboards/foo

	# All instances for a given kind(s):

	gcx resources get dashboards
	gcx resources get dashboards folders

	# Single resource kind, one or more resource instances:

	gcx resources get dashboards/foo
	gcx resources get dashboards/foo,bar

	# Single resource kind, long kind format:

	gcx resources get dashboard.dashboards/foo
	gcx resources get dashboard.dashboards/foo,bar

	# Single resource kind, long kind format with version:

	gcx resources get dashboards.v1alpha1.dashboard.grafana.app/foo
	gcx resources get dashboards.v1alpha1.dashboard.grafana.app/foo,bar

	# Multiple resource kinds, one or more resource instances:

	gcx resources get dashboards/foo folders/qux
	gcx resources get dashboards/foo,bar folders/qux,quux

	# Multiple resource kinds, long kind format:

	gcx resources get dashboard.dashboards/foo folder.folders/qux
	gcx resources get dashboard.dashboards/foo,bar folder.folders/qux,quux

	# Multiple resource kinds, long kind format with version:

	gcx resources get dashboards.v1alpha1.dashboard.grafana.app/foo folders.v1alpha1.folder.grafana.app/qux

	# Provider-backed resource types (SLO, Synthetic Monitoring, Alerting):

	gcx resources get slo
	gcx resources get slo/my-slo-uuid
	gcx resources get checks
	gcx resources get rules

	# Discover available JSON fields for a resource type:

	gcx resources get dashboards --json list

	# Select specific fields (no external parsing needed):

	gcx resources get dashboards --json metadata.name,spec.title
```

### Options

```
  -h, --help              help for get
      --json string       Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --on-error string   How to handle errors during resource operations:
                            ignore — continue processing all resources and exit 0
                            fail   — continue processing all resources and exit 1 if any failed (default)
                            abort  — stop on the first error and exit 1 (default "fail")
  -o, --output string     Output format. One of: json, text, wide, yaml (default "text")
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx resources](gcx_resources.md)	 - Manipulate Grafana resources

