## gcx policies validation-policies

[experimental] Validation policies and their bindings

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

Validation policies are written by Grafana apps, such as the rule policy and folder naming apps,
and enforced on writes through their bindings: Deny rejects a write, Warn admits it with a warning.

### Options

```
  -h, --help   help for validation-policies
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx policies](gcx_policies.md)	 - [experimental] Inspect and evaluate Grafana validation policies
* [gcx policies validation-policies evaluate](gcx_policies_validation-policies_evaluate.md)	 - [experimental] Evaluate validation policies against resources without writing them
* [gcx policies validation-policies list](gcx_policies_validation-policies_list.md)	 - [experimental] List validation policies and their bindings

