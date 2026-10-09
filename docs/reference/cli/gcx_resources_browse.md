## gcx resources browse

Interactively explore resources (read-only)

### Synopsis

Interactively explore resource types and their objects. Read-only.

The left pane lists resource types; the right pane shows the highlighted
type's spec schema. Press enter to list that type's objects, and the right
pane renders the highlighted object as YAML.

Keys: / filter (fuzzy) · enter select · esc back or clear filter ·
tab scroll the preview · o open the object in Grafana · q quit.

Selectors narrow the type list, using the same syntax as 'gcx resources get'.
Requires an interactive terminal; in agent mode or with piped I/O, use
'gcx resources list-types' and 'gcx resources get' instead.

```
gcx resources browse [RESOURCE_SELECTOR]... [flags]
```

### Examples

```

	# Explore every resource type
	gcx resources browse

	# Only dashboards and Synthetic Monitoring checks
	gcx resources browse dashboards checks.syntheticmonitoring
```

### Options

```
  -h, --help   help for browse
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from known agent identity variables. Set GCX_AGENT_NAME to identify a supported harness, or GCX_AGENT_MODE to control the mode.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx resources](gcx_resources.md)	 - Manipulate Grafana resources

