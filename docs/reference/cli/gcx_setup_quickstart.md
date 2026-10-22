## gcx setup quickstart

Bootstrap a contact point, a usage alert, and a usage dashboard.

### Synopsis

Create a small, idempotent starter set after first login: an email
contact point, a Grafana Cloud usage alert wired to it, and a dashboard of
the same active-series metric.

Re-running is safe — existing resources with the same names are left in place.
A step failure does not abort the remaining steps.

Interactive prompts require a TTY and are disabled in agent mode. Non-interactive
invocations must pass --use-defaults.

```
gcx setup quickstart [flags]
```

### Examples

```
  # Interactive first-time setup (TTY).
  gcx setup quickstart

  # Non-interactive with defaults.
  gcx setup quickstart --use-defaults --email you@example.com

  # Skip the dashboard.
  gcx setup quickstart --use-defaults --email you@example.com --dashboard=false

  # JSON result (agent / scripts).
  gcx setup quickstart --use-defaults --email you@example.com -o json
```

### Options

```
      --contact-point   Create the email contact point. Pass --contact-point=false to skip. Omit to use default (true).
      --dashboard       Create the usage dashboard. Pass --dashboard=false to skip. Omit to use default (true).
      --email string    Email address for the contact point. Required in non-interactive mode when the contact-point or usage-alert step is enabled.
  -h, --help            help for quickstart
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, text, yaml (default "text")
      --usage-alert     Create the usage alert rule. Pass --usage-alert=false to skip. Omit to use default (true).
      --use-defaults    Apply defaults without prompting (required when stdin is not a TTY)
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx setup](gcx_setup.md)	 - Onboard and configure Grafana Cloud products.
