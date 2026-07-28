## gcx kg entities correlate

Resolve the affected entities for a firing alert from its labels.

### Synopsis

Resolve the affected Knowledge Graph entities for one or more firing alerts.

Give it an alert's labels and a time window; it returns the entities the alert
hangs off, using the same correlation the product uses to attach alerts to
entities. This is the "I have an alert, which entity is it?" entry point — no
entity type/name or Cypher required.

Provide alerts as inline label sets, from an Alertmanager webhook file, or both:

  --alert-labels 'k=v,k=v'   one firing alert per flag (repeatable)
  -f/--file / stdin          Alertmanager JSON — an envelope {"alerts":[...]}
                             or a bare array [{"labels":{...}}], auto-detected

At least one of --alert-labels or -f is required. When nothing correlates the
command prints a notice and exits 0 (an empty result is not an error).

Note: correlation from a PromQL alert expression (--query) is not yet supported
here — the backend fallback is unreliable today; use alert labels instead.

```
gcx kg entities correlate [flags]
```

### Examples

```
  gcx kg entities correlate --alert-labels 'alertname=ErrorRatioBreach,job=cart' --since 1h
  gcx kg entities correlate -f am-webhook.json --since 1h
  cat am-webhook.json | gcx kg entities correlate -f - --since 6h
```

### Options

```
      --alert-labels stringArray   Firing alert label set as comma-separated key=value pairs; one flag per alert (repeatable)
  -f, --file string                Alertmanager webhook JSON file, or '-' for stdin (envelope {"alerts":[...]} or bare array [{"labels":...}])
      --from string                Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                       help for correlate
      --jq string                  jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string                Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string              Output format. One of: agents, json, table, yaml (default "table")
      --since string               Duration before --to (or now); mutually exclusive with --from/--to (default 1h; e.g. 1h, 30m, 7d)
      --to string                  End time (RFC3339, Unix timestamp, or relative like 'now')
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx kg entities](gcx_kg_entities.md)	 - Manage Knowledge Graph entities.

