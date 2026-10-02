---
name: agent-ping
description: >
  Set up and send Grafana mobile notifications from a local coding agent.
  Use when the user asks to get started with agent-ping, pair their phone for
  agent notifications, or says "ping me when you're done" or "let me know when
  you need me." Sends one-way notifications; does not receive phone replies.
---

# Agent ping

Use `gcx agent ping` to notify the user on their paired Grafana mobile app.
The command is experimental. Installation alone does not opt the user into
notifications: follow their request for this task or their established preference.

## Get started

### 1. Check the local CLI and context

```bash
gcx --version
gcx agent ping --help
gcx config view
```

If gcx is missing, use the bundled `setup-gcx` skill for installation, or read
it with `gcx agent skills get setup-gcx`. If `ping` is unknown, install a gcx
build containing agent-ping before continuing.

Identify the intended Grafana stack and named context (gcx's equivalent of a
profile). Preserve an existing working context. Use `--context` on checks and
pings when selecting a different one. Never use `config view --raw` to inspect
credentials.

### 2. Authenticate as the user

This endpoint sends to the authenticated user's phone. It requires a user
identity from **gcx login**, not `gcx cloud login` or a service account token.
For a Grafana Cloud stack, use browser OAuth if the user is not already signed in:

```bash
gcx login my-context --server https://example.grafana.net --oauth
gcx config check --context my-context
gcx api /api/user --context my-context
```

Replace the example context and URL with the user's intended stack. Let the
user complete browser sign-in. Verify the returned profile is the intended
user; a successful health check alone does not establish the right identity
or phone pairing. Do not create a service account to fix login problems.

### 3. Connect the phone

Have the user install the Grafana mobile app and sign in to the **same stack
and user account**. They can enter the stack URL in the app, or choose QR-code
sign-in and scan the code under their Grafana **Profile → Mobile App** tab.
Enable phone notification permissions. If QR sign-in is disabled by the
organization, use the stack URL and browser sign-in instead.

See Grafana's [mobile sign-in guide](https://grafana.com/docs/grafana-cloud/platform/mobile-app/install-and-sign-in/).
Pairing happens in Grafana and the mobile app; this command has no pairing-status
lookup or pairing operation. Do not claim pairing is verified from a config check.

### 4. Send one test ping

A request to set up agent-ping includes a test notification. Tell the user
you are sending it, then run:

```bash
gcx agent ping --text "Your local agent can reach you here." --title "Agent ping is ready" --context my-context
```

Ask whether it arrived. A successful command means the API accepted the request;
only the user's confirmation verifies delivery to their phone. Once confirmed,
the user can say: "Ping me when you're done, or if you need my input."

## Notify during work

Use the selected context consistently. Keep the message brief and include the
project or task so the user knows which session needs attention. Titles should
describe the event, such as "Ready for review", "Input needed", or "Task failed".

```bash
gcx agent ping --text "agent-ping: ready for review." --body "Tests passed. Return to the session for the changes." --title "Ready for review" --agent-name codex --context my-context
gcx agent ping --text "agent-ping: which deployment target should I use? Return to the session to choose." --title "Input needed" --agent-name codex --context my-context
```

Supply notification text with `--text`; positional messages and raw JSON input
are not accepted. All field values are plain text strings, including the body;
do not parse them as JSON or read file references from their values. The `text`
and `body` fields are separate. The command handles JSON escaping. Each payload
field has a flag:

| JSON field | Flag | Default |
|------------|------|---------|
| `text` | `--text` | Required |
| `body` | `--body` | Optional; omitted when empty |
| `title` | `--title` | `Agent ping` |
| `host` | `--host` | Local hostname |
| `agent` | `--agent-name` | `gcx` |
| `inbox` | `--inbox` | `agents` (only supported value) |

Agent metadata requires `inbox: agents`; the command includes it automatically.
When `body` is omitted, the backend uses `text` as the body.

Use `--agent-name` when the harness name is known. The global `--agent` flag
enables agent mode and does not set the sender's name.
Quote message arguments safely. For dynamically constructed invocations, pass
arguments directly through a subprocess argument array instead of interpolating
the message into shell code.

Send once for the requested completion, failure, or decision. Avoid progress
spam and duplicate pings for the same blocker. A phone notification does not
grant approval and does not collect an answer: ask the question in the session
and continue only work that does not depend on the answer.

Keep notification text suitable for a lock screen: omit credentials, raw logs,
and sensitive task details. Leave the detailed result in the agent session.

## If sending fails

- **400:** read the field validation errors. Agent metadata requires the Agents
  inbox. The backend limits title to 160 characters, text/body to 1500 each, and
  host/agent to 100 each. Control characters other than tab and newline are rejected.
- **401/403:** verify the selected context and user sign-in with `gcx login`.
  Cloud-platform credentials and service accounts do not replace a user identity.
- **404:** check the selected stack, endpoint availability, and phone pairing.
- **429:** wait before another attempt; do not loop or repeatedly notify.
- **Timeout or connection failure:** delivery may be unknown. Check whether the
  ping arrived before resending.
- **Accepted but absent:** check the mobile account, stack, notification permissions,
  and phone notification settings. Do not report delivery as verified.

Report a notification failure in the session without hiding the original task's
result. Avoid repeatedly retrying a failed ping or changing unrelated alerting
and escalation settings.
