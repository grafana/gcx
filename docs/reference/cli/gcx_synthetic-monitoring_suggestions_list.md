## gcx synthetic-monitoring suggestions list

List suggested checks generated from this stack's telemetry.

### Synopsis

List the checks the Synthetic Monitoring Reliability Inbox suggests for this stack.

Generating suggestions sends a summary of the stack's telemetry to an LLM and
is a paid, experimental service. Unlike the Synthetic Monitoring app, gcx cannot
check that Grafana Assistant is available or that its AI terms were accepted;
that is your responsibility before running this. Each run generates anew.

The service is deployed per region and is not available everywhere.

The service has no pagination and returns everything it generated (up to about
30 suggestions) in one response, so --limit only trims what is printed: it does
not reduce cost. Suggestions are kept in the service's order, highest confidence
first, which is not sorted by score. Use --json to print only some fields.

```
gcx synthetic-monitoring suggestions list [flags]
```

### Examples

```
  gcx synthetic-monitoring suggestions list
  gcx synthetic-monitoring suggestions list --limit 0 -o json
  gcx synthetic-monitoring suggestions list --json id,target,confidence,score
```

### Options

```
  -h, --help            help for list
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int       Maximum number of suggestions to print, in the service's order (highest confidence first); 0 for all (default 10)
  -o, --output string   Output format. One of: agents, json, table, yaml (default "table")
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

* [gcx synthetic-monitoring suggestions](gcx_synthetic-monitoring_suggestions.md)	 - Discover Synthetic Monitoring check suggestions.

