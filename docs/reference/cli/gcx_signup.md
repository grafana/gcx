## gcx signup

Create a Grafana Cloud account and connect gcx to it

### Synopsis

Create a free Grafana Cloud account in the browser and connect gcx to the
account's first stack.

gcx opens the Grafana Cloud sign-up page and waits. In the browser, create the
account, verify your email (the emailed link may open a new tab), create your
first stack, and approve "Connect gcx". The browser then returns to gcx, which
saves the connection. No stack URL or token is needed. If the browser loses
the page, press Enter in a local terminal to open it again, or open the
printed URL.

The connection is saved to CONTEXT_NAME. Without it, gcx uses the current
context, or a context named "default" when none is set. signup only ever saves
a new connection: it refuses, before the browser opens, a context that already
has a stack or Grafana Cloud entry, or a name that an existing stack entry
uses. Pass an unused CONTEXT_NAME instead. For an account you already have,
run gcx login.

A person completes the browser steps. In agent mode gcx prints the URL instead
of opening the browser, and asks no questions. signup does not save Grafana
Cloud management credentials; run gcx cloud login for those. If signup fails
once the browser step has started, the error shows the gcx login command that
finishes the connection; do not run signup again, which would start a second
account.

```
gcx signup [CONTEXT_NAME] [flags]
```

### Examples

```
  gcx signup
  gcx signup my-stack
  gcx signup --oauth-manual
```

### Options

```
      --config string             Path to the configuration file to use
      --context string            Name of the context to use
  -h, --help                      help for signup
      --jq string                 jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string               Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --oauth-callback-port int   Fixed local port for the OAuth callback server (default: auto-pick from 54321-54399). Useful when only specific ports are forwarded between a remote host and your browser
      --oauth-manual              Complete browser OAuth without a local callback server: gcx prints the URL, then reads the redirect URL that you copy from the browser address bar. Use this when gcx runs on a remote host and the browser runs on your own computer
  -o, --output string             Output format. One of: agents, json, text, yaml (default "text")
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations

