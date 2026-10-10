## gcx assistant search

Search historical and indexed Grafana evidence.

### Synopsis

Search infrastructure memories, dashboards (including panel queries), past
investigations, mostly resolved incidents, and alert-rule definitions.
Choose the smallest set of collections that answers your question.

For investigations and incidents, omitting --from and --to searches the last
three calendar months. Supplying either flag replaces that default; the omitted
side is unbounded. These flags do not affect other collections.

Search runs on the server in retrieval-only mode: no generative query rewriting,
relevance filtering, or enrichment agent. Results are ranked and capped by the
backend, not exhaustive; total counts returned hits, not all matching resources.
Collection errors and disabled collections are preserved in the output. A partial
failure exits 4; failure of every searched collection exits 1.

For current telemetry use metrics/logs/traces query; for current alert state or
active incidents use alert and IRM commands. Use assistant prompt for reasoning.
The OAuth Grafana proxy currently requires grafana-api:write for this POST
endpoint, even though retrieval is read-only.

```
gcx assistant search <query> [flags]
```

### Examples

```
  gcx assistant search "checkout latency" --collections dashboards
  gcx assistant search "checkout timeouts" --collections investigations,incidents --from now-1y
  gcx assistant search "checkout dependencies" --collections infrastructure -o json
```

### Options

```
      --collections strings   Required comma-separated collections (case-insensitive): infrastructure, dashboards, investigations, incidents, alertRules; searches the union server-side
      --from string           Include investigations and incidents created at or after this time (RFC3339, Unix timestamp, or relative like 'now-30d'); when both time flags are omitted, the backend searches the last three calendar months
  -h, --help                  help for search
      --jq string             jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string           Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string         Output format. One of: agents, json, text, yaml (default "text")
      --timeout int           Maximum time in seconds for configuration resolution and search (default 60)
      --to string             Include investigations and incidents created before this time (exclusive; RFC3339, Unix timestamp, or relative like 'now'); supplying either time flag removes the backend's default lower bound
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

* [gcx assistant](gcx_assistant.md)	 - Interact with Grafana Assistant

