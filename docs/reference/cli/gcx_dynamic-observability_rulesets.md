## gcx dynamic-observability rulesets

[experimental] Inspect and control probe rulesets

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.

Inspect and control probe rulesets.

### Options

```
  -h, --help   help for rulesets
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

* [gcx dynamic-observability](gcx_dynamic-observability.md)	 - [experimental] Inspect Dynamic Observability probes and agents
* [gcx dynamic-observability rulesets list](gcx_dynamic-observability_rulesets_list.md)	 - [experimental] List rulesets with current attachment status
* [gcx dynamic-observability rulesets pause](gcx_dynamic-observability_rulesets_pause.md)	 - [experimental] pause a ruleset
* [gcx dynamic-observability rulesets resume](gcx_dynamic-observability_rulesets_resume.md)	 - [experimental] resume a ruleset
* [gcx dynamic-observability rulesets status](gcx_dynamic-observability_rulesets_status.md)	 - [experimental] Show a ruleset's node and target attachment status

