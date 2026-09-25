## gcx slo definitions push

Push SLO from files (Deprecated: use gcx resources push).

### Synopsis

Push SLO from files.

Deprecated: use gcx resources push slos.v1alpha1.slo.ext.grafana.app -p PATH instead.
Writes update only an existing metadata.name UUID; an absent or unknown UUID creates a new resource.
Manifests may omit apiVersion and kind; this command supplies its resource type.
This compatibility command retains its file-at-a-time results and local-only --dry-run preview.
The preview shows manifest identities only; it does not resolve the remote UUID or determine create versus update.

```
gcx slo definitions push FILE... [flags]
```

### Options

```
      --dry-run         Preview changes without making them
  -h, --help            help for push
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of dotted field paths to include in JSON output (e.g. spec.name), or 'list' (or '?') to discover the available paths
  -o, --output string   Output format. One of: agents, json, text, yaml (default "text")
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx slo definitions](gcx_slo_definitions.md)	 - Manage SLO definitions.

