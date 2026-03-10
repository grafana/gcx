## grafanactl dev import

Import resources from Grafana and convert them to Go builder code

### Synopsis

Import resources from a Grafana instance and convert them into Go files using the grafana-foundation-sdk builder pattern. Each imported resource is written as a function returning *resource.ManifestBuilder.

```
grafanactl dev import [RESOURCE_SELECTOR]... [flags]
```

### Examples

```

	# Import all dashboards into the default path (imported/):
	grafanactl dev import dashboards

	# Import a specific dashboard by name:
	grafanactl dev import dashboards/my-dashboard

	# Import multiple resource types:
	grafanactl dev import dashboards folders

	# Import into a custom directory:
	grafanactl dev import dashboards --path src/grafana

```

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for import
  -p, --path string      Import path. (default "imported")
```

### Options inherited from parent commands

```
      --agent           Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GRAFANACTL_AGENT_MODE env vars.
      --no-color        Disable color output
      --no-truncate     Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count   Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [grafanactl dev](grafanactl_dev.md)	 - Manage Grafana resources as code

