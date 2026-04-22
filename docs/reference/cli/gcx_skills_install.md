## gcx skills install

Install bundled gcx skills into ~/.agents/skills

### Synopsis

Install one or more bundled gcx Agent Skills into a user-level .agents directory for tools that follow the .agents skill convention. Use --all to install the entire bundle.

```
gcx skills install [SKILL]... [flags]
```

### Examples

```
  gcx skills install setup-gcx
  gcx skills install setup-gcx debug-with-grafana explore-datasources
  gcx skills install --all
  gcx skills install --all --dry-run
  gcx skills install setup-gcx --force
```

### Options

```
      --all             Install all bundled skills
      --dir string      Root directory for the .agents installation (default "~/.agents")
      --dry-run         Preview the installation without writing files
      --force           Overwrite existing differing files managed by the gcx skills bundle
  -h, --help            help for install
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: json, text, yaml (default "text")
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --context string     Name of the context to use (overrides current-context in config)
      --limit int          Maximum number of items to return from list operations (0 for all; defaults to 50 in agent mode)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx skills](gcx_skills.md)	 - Manage portable gcx Agent Skills

