## gcx alert time-intervals update

Replace an existing time interval using its fetched version.

### Synopsis

The argument selects the current title; the manifest supplies the desired title.
A different spec.name explicitly renames the selected interval. The manifest must
carry a fetched metadata.resourceVersion and, when present, the selected object's
API name. Stale versions fail with conflict; missing titles are never created.
Fetch the new title after rename to refresh local metadata; files are unchanged.

```
gcx alert time-intervals update <current-title> -f <file> [flags]
```

### Examples

```
  gcx alert time-intervals get weekends -o yaml > interval.yaml
  gcx alert time-intervals update weekends -f interval.yaml
  gcx alert time-intervals get weekends -o yaml | gcx alert time-intervals update weekends -f -
```

### Options

```
      --api-version string   Must match the manifest's apiVersion when set; the manifest decides the version
  -f, --filename string      Path to one native JSON/YAML TimeInterval manifest ('-' reads stdin)
  -h, --help                 help for update
      --jq string            jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string        Output format. One of: agents, json, text, yaml (default "text")
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from known agent identity variables. Set GCX_AGENT_NAME to identify a supported harness, or GCX_AGENT_MODE to control the mode.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx alert time-intervals](gcx_alert_time-intervals.md)	 - Manage notification time intervals by title.

