## gcx agent ping

[experimental] Send a notification to your paired phone

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.

Send a one-way notification to the authenticated user's paired Grafana mobile app.
Set the payload fields with --text (required), --body, --title, --host, and
--agent-name. All message fields are plain text strings. --text and --body set
separate JSON fields; an omitted body is not sent.
Agent metadata requires the Agents inbox, so --inbox defaults to agents (the
only supported value). The backend uses text as the body when body is omitted.
The --agent-name flag sets the JSON agent field; the global --agent flag enables
agent mode. The command constructs and escapes the JSON request automatically.
Sign in with gcx login using your user identity, and connect the mobile app to
the same Grafana stack and user. gcx cloud login and service account tokens do
not provide the user identity required by this endpoint.

Success means Grafana accepted the request, not that the phone received or read
it. This command does not wait for a reply.

```
gcx agent ping [flags]
```

### Examples

```
  gcx agent ping --text "Tests passed. Ready for review."
  gcx agent ping --text "Which deployment target should I use?" --title "Input needed"
  gcx agent ping --text "Build finished" --body "All tests passed. Return to the session for details." --agent-name codex --context my-context
```

### Options

```
      --agent-name string   Name of the agent sending the notification, as plain text (default "gcx")
      --body string         Additional notification body as plain text (optional)
      --config string       Path to the configuration file to use
      --context string      Name of the context to use
  -h, --help                help for ping
      --host string         Source machine name as plain text (default: local hostname)
      --inbox string        Notification inbox (only agents is supported) (default "agents")
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, json, text, yaml (default "text")
      --text string         Notification text as plain text (required)
      --title string        Notification title as plain text (default "Agent ping")
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

* [gcx agent](gcx_agent.md)	 - Utilities for AI agents

