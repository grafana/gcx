---
title: Use gcx with AI agents
labels:
  products:
    - cloud
    - enterprise
    - oss
description: How gcx behaves when an AI coding agent runs it, and how to set up skills, sign-in, destructive operations, detection, and output for agents.
keywords:
  - gcx
  - Grafana CLI
  - agents
  - agent mode
  - AI
weight: 9
---

# Use `gcx` with AI agents

With `gcx`, your AI coding agent can use production data to troubleshoot issues and make better implementation decisions. For example, the agent can examine traces alongside your code to identify which operation to optimize, or ask Grafana Assistant to investigate a potential issue.

Both people and agents use the same `gcx` commands. However, when `gcx` detects an agent, it switches to [agent mode](#how-agent-mode-works).

## Before you begin

- [Install `gcx`](../installation/) where your agent runs.
- [Authenticate `gcx`](../authentication/). If the agent runs without a person present, refer to [Sign in for agents](#sign-in-for-agents).

## How agent mode works

`gcx` turns on agent mode automatically when it finds an environment variable set by a supported agent tool, such as Claude Code, Cursor, GitHub Copilot, Codex, or Gemini CLI.

The main differences between modes are the following:

| | Without agent mode | In agent mode |
| --- | --- | --- |
| Default output | Readable tables | Compact JSON (spills to a temp file above 100 KiB) |
| Color and table truncation | On in a terminal | Off |
| Errors | A readable message on `stderr` | A JSON error document on `stdout` |
| Destructive operations | An interactive confirmation prompt | Rejected unless you allow them. Refer to [Approve destructive operations](#approve-destructive-operations). |

In agent mode, `gcx` also writes data to `stdout` and hints and progress messages to `stderr`, so an agent can parse `stdout` without filtering. Outside agent mode, you can get the same plain output with `--no-color` and `--no-truncate`, for example on a CI runner that reports a terminal.

You can override these defaults with explicit flags. For example, `-o text` prints a table in agent mode, and `-o json` prints JSON without agent mode. For manual overrides and output details, refer to [Customize detection, output, and error handling](#customize-detection-output-and-error-handling).

## Sign in for agents

An agent can do anything that its `gcx` credential allows, so make sure you use a credential with only the permissions the agent's tasks need. Refer to [Roles and permissions](../permissions/).

**When a person is present**, use browser-based OAuth. `gcx` opens a browser and waits for you to approve the sign-in:

```bash
gcx login my-stack --server https://my-stack.grafana.net --oauth
```

- If the agent's environment can't receive the browser callback, add `--oauth-manual`. `gcx` prints the URL, and you paste back the redirect URL.
- If the agent runs `gcx` in a sandbox that blocks writes to the operating system credential store, `gcx` stops before the browser opens. Approve the command to run outside the sandbox. Refer to [Restricted execution sessions](../keychain/#restricted-execution-sessions).

**When no person is present**, such as in CI or a scheduled job, use a [service account token](../authentication/#service-account-token). Set it with [environment variables](../authentication/#environment-variables), or save it to a context:

```bash
gcx login my-stack --server https://my-stack.grafana.net --token <SERVICE_ACCOUNT_TOKEN> --yes
```

## Install `gcx` skills for agents

`gcx` ships agent skills: task-specific instructions that teach an agent common workflows, such as setting up `gcx`, creating dashboards, investigating alerts, and managing SLOs. For the full list, refer to the [skill inventory](https://github.com/grafana/gcx/blob/main/claude-plugin/README.md#skills).

**For Claude Code**, install the `gcx` plugin:

```text
/plugin marketplace add grafana/gcx
/plugin install gcx@gcx-marketplace
```

**For other tools that use the `.agents` skill convention**, such as OpenAI Codex, OpenCode, and Pi, use `gcx agent skills`:

```bash
# List the bundled skills and whether they're installed
gcx agent skills list

# Install all skills into ~/.agents/skills (use --dir for another .agents root)
gcx agent skills install --all

# Refresh installed skills after you upgrade gcx
gcx agent skills update

# Remove a skill
gcx agent skills uninstall <SKILL>
```

`gcx agent skills update` doesn't add skills that are new in the bundle. To add them, run `gcx agent skills install --all` again.

#### How agents discover skills and commands

If your tool doesn't use the Claude Code plugin or `~/.agents/skills` (for example in Cursor, GitHub Copilot, or a custom agent integration), you don't need to install any files. Your agent can read bundled skills on demand and discover commands progressively:

```bash
# Print a bundled skill without writing files to disk
gcx agent skills get <SKILL>

# Orient on top-level command groups in a compact text tree
gcx help-tree --depth 1 -o text

# Drill into one command group with inline arguments, flags, and agent hints
gcx help-tree resources -o text

# Output the full JSON catalog of commands, cost estimates, and resource types
gcx commands
```

Commands follow a consistent `gcx <area> <noun> <verb>` grammar, for example `gcx slo definitions list`, so agents can predict commands they haven't seen. Every command also has `--help` text with commented examples that agents can read to learn the syntax.

## How agents manage resources safely

`gcx` includes features that let an agent preview changes, retry safely, and recover from partial failures. You decide which destructive operations an agent can run.

### Preview changes

Agents use `--dry-run` to preview a change before they make it. Commands that change resources, such as `gcx resources push` and `gcx resources delete`, support the flag. With `--dry-run`, the command simulates the operation and reports what it would do, without making changes:

```bash
gcx resources push dashboards --dry-run
```

Not every command that changes data has `--dry-run`. Run `gcx <command> --help` to check.

### Retry safely

`gcx resources push` is idempotent: it creates a resource if it doesn't exist and updates it if it does. An agent can run the same push again after a failure without creating duplicates.

### Handle batch failures

When a command processes several resources, agents can use the `--on-error` flag to control what happens if one fails:

| Value | Behavior |
| --- | --- |
| `fail` (default) | Processes all resources, then exits with code `4` if any failed. |
| `ignore` | Processes all resources and exits with code `0`. |
| `abort` | Stops at the first error and exits with a non-zero code. |

The `--max-concurrent` flag limits how many operations run at once. The default is 10.

### Approve destructive operations

Agent mode never approves a destructive operation by itself. Commands that require a confirmation prompt to a person fail in agent mode unless you allow the operation in one of these ways:

- Pass `--force` to that command.
- Set `GCX_AUTO_APPROVE=true` in the agent's environment. `GCX_AUTO_APPROVE` accepts `1`, `t`, or `true`. Other values, such as `yes`, make the command fail.

`gcx resources delete` uses its own guard instead of a prompt: deleting every resource of a type requires `--force` or `--yes`.

Ask your agent to check with you before it adds `--force`, and avoid setting `GCX_AUTO_APPROVE` in a shell that people also use.

## How agents read `gcx` output

In agent mode, `gcx` returns structured output that agents can parse, and tells them when a result is incomplete or a command failed.

### Select fields and filter results

Agents can shape the output inside `gcx` with `--json` and `--jq`, instead of piping it to other tools. This keeps unneeded data out of the agent's context:

```bash
# List the fields that the command can return
gcx resources get dashboards --json list

# Return only some fields
gcx resources get dashboards --json metadata.name,spec.title

# Transform the output with a jq expression
gcx resources get dashboards --jq '.items | length'
```

### Detect incomplete lists

List commands never truncate results silently. The `--limit` flag sets how many items a command returns, and `--limit 0` returns all items. For example, `gcx resources get` returns up to 50 items for each resource type by default.

When a command returns only part of the results, it prints a hint to `stderr` with the command that returns more. Some list commands also add a `list_meta` object to the JSON output, so an agent can tell that the result is incomplete and which command to run next:

```json
"list_meta": {"truncated": true, "returned": 50, "continue": "gcx ... list --limit 100"}
```

### Handle large results

When a result is larger than 100 KiB, `gcx` writes the full result to a temporary file named `gcx-results-*.json` and prints a short receipt to `stdout` instead:

```json
{
  "type": "gcx.spill_reference",
  "schema_version": "1",
  "spilled_to": "/tmp/gcx-results-3781234567.json",
  "bytes": 143200,
  "content_format": "json",
  "total_items": 312,
  "preview_sample": [
    {
      "metadata": {
        "name": "prod-overview"
      }
    }
  ],
  "message": "Response too large for stdout (143200 bytes). Full data written to /tmp/gcx-results-3781234567.json. Read that file for complete results, or rerun with -o json to force inline output."
}
```

For list responses, `preview_sample` contains only the first 3 items (or sorted top-level keys for objects)—never the complete result. Read the file at `spilled_to` for the full data. When `--jq` is combined with `-o agents`, oversized `jq` streams spill to `gcx-results-*.jsonl` with `content_format` set to `"jsonl"`, `total_values` instead of `total_items`, and `preview_sample` set to `null`.

You can control this behavior in the following ways:

- Agents can pass `-o json` to write the full JSON to `stdout` without spilling.
- Agents can redirect `stdout` to a file, for example `gcx resources get dashboards > dashboards.json`, to write the full result to that file.
- Set `GCX_AGENT_SPILL_BYTES` in the agent's environment to change the threshold in bytes. Set it to `0` to turn off spilling.
- Run `gcx agent prune` to remove spill files older than 30 minutes, for example at the end of an agent session. Agents can also run it themselves.

### Handle errors and exit codes

In agent mode, a failed command writes one JSON error document to `stdout`, and the `exitCode` field matches the process exit code:

```json
{"type": "gcx.error", "schema_version": "1", "error": {"summary": "Resource not found", "exitCode": 1}}
```

The `error` object can also include `details`, `suggestions`, and `docsLink`. When a batch operation partly succeeds, the document has the type `gcx.partial_result` and includes the successful `items` alongside the `error`.

Agents use the process exit code to decide what to do next:

| Code | Meaning | What an agent can do |
| --- | --- | --- |
| `0` | Success | Continue. |
| `1` | General error | Read the error `summary` and `suggestions`. |
| `2` | Usage error, such as a bad flag or selector | Correct the command. |
| `3` | Authentication or authorization failure | Stop and ask you to sign in or change permissions. |
| `4` | Partial failure | Read the `gcx.partial_result` document and retry the failed items. |
| `5` | Cancelled | Ask you before it retries. |
| `6` | Grafana version not supported | Stop and report the version. |

Run `gcx help exit-codes` for descriptions and examples of each code.

## Configure agent mode

### Override agent mode detection

If your tool isn't detected automatically, set `GCX_AGENT_NAME` to a supported label in the agent's command environment:

```bash
GCX_AGENT_NAME=crush gcx resources get dashboards
```

Set the label only in the agent's environment, not in a shell that people also use. Supported labels for `GCX_AGENT_NAME` (and the shared `AI_AGENT` variable) are `aider`, `amazon-q`, `amp`, `augment`, `claude-code`, `cline`, `codex`, `crush`, `cursor`, `devin`, `factory-droid`, `gemini-cli`, `github-copilot`, `goose`, `hermes`, `junie`, `kilo-code`, `kiro`, `openclaw`, `opencode`, `openhands`, `pi`, and `qwen-code`. Refer to [Environment variables](../cli-reference/#environment-variables) for variable details and precedence.

To turn agent mode on or off regardless of detection:

| To | Use |
| --- | --- |
| Turn agent mode on | `--agent` or `GCX_AGENT_MODE=1` |
| Turn agent mode off | `--agent=false` or `GCX_AGENT_MODE=0` |

The `--agent` flag takes precedence over environment variables. `GCX_AGENT_MODE` accepts `1`, `true`, or `yes` to turn agent mode on, and `0`, `false`, or `no` to turn it off. Other values, such as `off`, are ignored.

### Log failed agent calls

To record failed agent-mode calls locally for debugging, enable `diagnostics.agent-invocation-log` in your [configuration](../configuration/#configuration-reference):

```bash
gcx config set diagnostics.agent-invocation-log true
```

When enabled, `gcx` writes failed agent-mode invocations (up to 1,000 JSONL records) to `~/.local/state/gcx/agent-invocation-errors.jsonl` (`$XDG_STATE_HOME/gcx/agent-invocation-errors.jsonl`), or to `diagnostics.log-dir` if configured.

## Learn more

- [Diagnose missing telemetry](../guides/diagnose-missing-telemetry/): An example workflow that uses an agent with `gcx` skills.
- [Usage statistics](../anonymous-usage-statistics/#agent-identity): What `gcx` reports about agent use, and how to opt out.
