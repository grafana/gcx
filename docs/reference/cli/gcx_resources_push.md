## gcx resources push

Push resources to Grafana

### Synopsis

Push resources to Grafana using a specific format. See examples below for more details.

```
gcx resources push [RESOURCE_SELECTOR]... [flags]
```

### Examples

```

	# Everything:

	gcx resources push

	# All instances for a given kind(s):

	gcx resources push dashboards
	gcx resources push dashboards folders

	# Single resource kind, one or more resource instances:

	gcx resources push dashboards/foo
	gcx resources push dashboards/foo,bar

	# Single resource kind, long kind format:

	gcx resources push dashboard.dashboards/foo
	gcx resources push dashboard.dashboards/foo,bar

	# Single resource kind, long kind format with version:

	gcx resources push dashboards.v1alpha1.dashboard.grafana.app/foo
	gcx resources push dashboards.v1alpha1.dashboard.grafana.app/foo,bar

	# Multiple resource kinds, one or more resource instances:

	gcx resources push dashboards/foo folders/qux
	gcx resources push dashboards/foo,bar folders/qux,quux

	# Multiple resource kinds, long kind format:

	gcx resources push dashboard.dashboards/foo folder.folders/qux
	gcx resources push dashboard.dashboards/foo,bar folder.folders/qux,quux

	# Multiple resource kinds, long kind format with version:

	gcx resources push dashboards.v1alpha1.dashboard.grafana.app/foo folders.v1alpha1.folder.grafana.app/qux

	# Provider-backed resource types (SLO, Synthetic Monitoring, Alerting):

	gcx resources push slo -p ./slo-defs/
	gcx resources push checks -p ./checks/
	gcx resources push rules -p ./rules/

	# Mixed push: native and provider resources from the same directory
	# (types auto-detected from apiVersion/kind in YAML files):

	gcx resources push -p ./resources/
```

### Options

```
      --dry-run               If set, the push operation will be simulated, without actually creating or updating any resources
  -h, --help                  help for push
      --include-managed       If set, resources managed by other tools will be included in the push operation
      --max-concurrent int    Maximum number of concurrent operations (default 10)
      --omit-manager-fields   If set, the manager fields will not be appended to the resources
      --on-error string       How to handle errors during resource operations:
                                ignore — continue processing all resources and exit 0
                                fail   — continue processing all resources and exit 1 if any failed (default)
                                abort  — stop on the first error and exit 1 (default "fail")
  -p, --path strings          Paths on disk from which to read the resources to push (default [./resources])
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

* [gcx resources](gcx_resources.md)	 - Manipulate Grafana resources

