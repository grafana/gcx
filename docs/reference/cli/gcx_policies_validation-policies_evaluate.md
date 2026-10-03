## gcx policies validation-policies evaluate

[experimental] Evaluate validation policies against resources without writing them

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

Evaluate the namespace's validation policies in-process, against local manifests (--path) or
against resources already stored in Grafana (resource selectors). Nothing is written.

Policies, bindings and their parameter objects are read from Grafana, and policies are
type-checked against Grafana's schemas, so the outcome matches what Grafana would decide if each
resource were created now. That also finds stored resources that predate a policy.

Exits with code 4 when any resource breaks a policy whose binding denies writes.

```
gcx policies validation-policies evaluate [RESOURCE_SELECTOR]... [flags]
```

### Examples

```

	# Would these manifests be admitted?
	gcx policies validation-policies evaluate -p ./resources

	# Only the alert rules among them
	gcx policies validation-policies evaluate -p ./resources alertrules

	# Which stored alert rules and folders break a policy?
	gcx policies validation-policies evaluate alertrules folders
```

### Options

```
  -h, --help                 help for evaluate
      --jq string            jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --max-concurrent int   Maximum number of concurrent operations (default 10)
  -o, --output string        Output format. One of: agents, json, text, yaml (default "text")
  -p, --path strings         Paths on disk from which to read the resources to evaluate. Without it, resources are read from Grafana.
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

* [gcx policies validation-policies](gcx_policies_validation-policies.md)	 - [experimental] Validation policies and their bindings

