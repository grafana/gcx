## grafanactl resources delete

Delete resources from Grafana

### Synopsis

Delete resources from Grafana.

```
grafanactl resources delete [RESOURCE_SELECTOR]... [flags]
```

### Examples

```

	# Delete a single dashboard
	grafanactl resources delete dashboards/some-dashboard

	# Delete multiple dashboards
	grafanactl resources delete dashboards/some-dashboard,other-dashboard

	# Delete a dashboard and a folder
	grafanactl resources delete dashboards/some-dashboard folders/some-folder

	# Delete every dashboard
	grafanactl resources delete dashboards --force

	# Delete every resource defined in the given directory
	grafanactl resources delete -p ./unwanted-resources/

	# Delete every dashboard defined in the given directory
	grafanactl resources delete -p ./unwanted-resources/ dashboard

	# Delete all dashboards with auto-approval
	grafanactl resources delete dashboards --yes

	# Delete all dashboards using environment variable
	GRAFANACTL_AUTO_APPROVE=1 grafanactl resources delete dashboards

```

### Options

```
      --dry-run              If set, the delete operation will be simulated
      --force                Delete all resources of the specified resource types
  -h, --help                 help for delete
      --max-concurrent int   Maximum number of concurrent operations (default 10)
      --on-error string      How to handle errors during resource operations:
                               ignore — continue processing all resources and exit 0
                               fail   — continue processing all resources and exit 1 if any failed (default)
                               abort  — stop on the first error and exit 1 (default "fail")
  -p, --path strings         Path on disk containing the resources to delete
  -y, --yes                  Auto-approve destructive operations (automatically enables --force)
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

