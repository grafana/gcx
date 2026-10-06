## gcx dashboards search

Search dashboards by text or meaning.

### Synopsis

Search dashboards using lexical search, or combine keyword and semantic
search with --hybrid to find dashboards by their content, including panels.

Hybrid search requires query text and supports --folder and --limit (1-200).
It returns the top matches in relevance order, not an exhaustive inventory;
there is no pagination. JSON/YAML results include spec.score and spec.chunks.
Scores are comparable only within one response. Use -o wide to see the best
matching chunk in the table. --tag, --sort and --deleted require lexical search.
If hybrid search is unavailable on your instance, retry without --hybrid.

Both endpoints use v0alpha1 and do not support --api-version overrides.
Lexical search accepts an empty query with at least one --folder or --tag.

```
gcx dashboards search [query] [flags]
```

### Examples

```
  # Search by title.
  gcx dashboards search "my dashboard"

  # Search within a folder.
  gcx dashboards search --folder my-folder-name

  # Search by tag with multiple folders.
  gcx dashboards search --tag prod --folder folder-a --folder folder-b

  # Find dashboards by meaning and panel content.
  gcx dashboards search "Kubernetes memory issues" --hybrid --limit 10 -o json

  # Output as YAML.
  gcx dashboards search "metrics" -o yaml
```

### Options

```
      --deleted              Include recently deleted dashboards
      --folder stringArray   Filter server-side by folder UID (repeatable, matches any; hybrid: empty matches root)
  -h, --help                 help for search
      --hybrid               Combine keyword and semantic search, including dashboard panel content
      --jq string            jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int            Maximum number of results (hybrid: 1-200; lexical: 0 for no limit) (default 50)
  -o, --output string        Output format. One of: agents, json, table, wide, yaml (default "table")
      --sort string          Sort key (e.g. name_sort)
      --tag stringArray      Filter by tag (repeatable)
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

* [gcx dashboards](gcx_dashboards.md)	 - Manage Grafana dashboards

