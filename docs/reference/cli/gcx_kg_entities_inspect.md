## gcx kg entities inspect

Show detailed info, insights, and summary for a single entity, including a link to the RCA Workbench.

```
gcx kg entities inspect [Type--Name] [flags]
```

### Options

```
      --env string                         Environment scope (run 'gcx kg meta scopes' to see valid values)
      --from string                        Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                               help for inspect
      --insight-categories strings         Filter insights by category (comma-separated, e.g. saturation,anomaly,failure); empty = all categories
      --insight-hide-chronic-above int     Hide insights present more than this percent of the window (0-100); overrides --insight-hide-noise on this axis
      --insight-hide-noise                 Apply RCA Workbench noise filter: hide insights older than 48h or present >90% of the window
      --insight-hide-older-than duration   Hide insights older than a whole number of hours (e.g. 24h); overrides --insight-hide-noise on this axis
      --json string                        Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --name string                        Entity name
      --namespace string                   Namespace scope (run 'gcx kg meta scopes' to see valid values)
      --open                               Open the entity in the RCA Workbench in your browser
  -o, --output string                      Output format. One of: agents, json, yaml (default "json")
      --share-link                         Print the RCA Workbench URL for this entity to stderr
      --since string                       Duration before --to (or now); mutually exclusive with --from (e.g. 1h, 30m, 7d)
      --site string                        Site scope (run 'gcx kg meta scopes' to see valid values)
      --to string                          End time (RFC3339, Unix timestamp, or relative like 'now')
      --type string                        Entity type (run 'gcx kg meta schema' to see available types)
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use (overrides current-context in config)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx kg entities](gcx_kg_entities.md)	 - Manage Knowledge Graph entities.

