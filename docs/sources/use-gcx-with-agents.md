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

Both people and agents use the same `gcx` commands. However, when `gcx` detects an agent, it switches to **agent mode**.

Agent mode **reduces token usage**. `gcx` keeps output compact, lets the agent request only the fields it needs, and keeps large results out of the agent's context window.


## How agent mode works

`gcx` turns on agent mode automatically when it finds an environment variable set by a supported agent tool, such as Claude Code, Cursor, GitHub Copilot, Codex, or Gemini CLI.

The main differences between modes are the following. Most of them reduce the tokens an agent spends reading output:

| | Without agent mode | In agent mode |
| --- | --- | --- |
| Default output | Readable tables | [Compact JSON](#parse-compact-json-by-default) |
| Color and table truncation | Enabled in a terminal | Disabled |
| Errors | A readable message on `stderr` | A JSON error document on `stdout` |
| Status messages and hints | Text on `stderr` | Text on `stderr`, with hints as JSON lines, so `stdout` carries only the result |
| Large results | Printed in full | [Saved to a temporary file](#handle-large-results), with a short receipt on `stdout` |
| Destructive operations | An interactive confirmation prompt | [Rejected unless you allow them](#approve-destructive-operations) |

Agents also rely on `gcx` features that are available in both modes:

| Feature | How agents use it |
| --- | --- |
| `--dry-run` | [Preview changes](#preview-changes) |
| `gcx resources push` | [Retry safely](#retry-safely) after a failure |
| Exit codes and JSON errors | [Handle errors and exit codes](#handle-errors-and-exit-codes) |
| `--on-error` | [Handle batch failures](#handle-batch-failures) |
| `--json` and `--jq` | [Select fields and filter results](#select-fields-and-filter-results) to get only the data they need |
| `--limit` and `list_meta` | [Detect incomplete lists](#detect-incomplete-lists) to continue processing results |

## Before you begin

[Install `gcx`](../installation/) in the environment where your agent runs.

### Authenticate `gcx` for your agent

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

For additional details, refer to [Authenticate `gcx`](../authentication/).

{{< admonition type="caution" >}}

Your agent can perform any action its `gcx` credential allows. Use a credential with only the permissions the agent needs for its tasks. Refer to [Roles and permissions](../permissions/).

{{< /admonition >}}

### Install `gcx` agent skills

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

When a command processes several resources at once, such as `gcx resources push` or `gcx resources delete`, one failure doesn't have to stop the rest. Agents use the `--on-error` flag to choose what happens after a resource fails:

| Value | What happens | Exit code |
| --- | --- | --- |
| `fail` (default) | `gcx` continues with the remaining resources, then reports that some failed. | `4` |
| `ignore` | `gcx` continues with the remaining resources and doesn't treat failures as errors. | `0` |
| `abort` | `gcx` stops at the first failure. | Non-zero |

With the default, an agent sees exit code `4` and knows that some resources succeeded and some failed. Refer to [Handle errors and exit codes](#handle-errors-and-exit-codes).

### Approve destructive operations

Agent mode never approves a destructive operation by itself. Commands that require a confirmation prompt to a person fail in agent mode unless you allow the operation in one of these ways:

- Pass `--force` to that command.
- Set `GCX_AUTO_APPROVE=true` in the agent's environment. `GCX_AUTO_APPROVE` accepts `1`, `t`, or `true`. Other values, such as `yes`, make the command fail.

`gcx resources delete` uses its own guard instead of a prompt: deleting every resource of a type requires `--force` or `--yes`.

Ask your agent to check with you before it adds `--force`, and avoid setting `GCX_AUTO_APPROVE` in a shell that people also use.

## How agents read `gcx` output

In agent mode, `gcx` returns structured output, reports incomplete results and errors, and suggests next steps.

### Parse compact JSON by default

The default output in agent mode is compact JSON instead of tables. It has no colors, no layout, and no truncated columns, so agents can parse it directly without spending tokens on formatting.

`gcx` writes the data to `stdout` and status messages and hints to `stderr`. Agents can parse `stdout` without filtering out those messages.

### Select fields and filter results

Agents can shape the output inside `gcx` with `--json` and `--jq`, instead of piping it to other tools. This saves tokens by keeping unneeded data out of the agent's context:

```bash
# List the fields that the command can return
gcx resources get dashboards --json list

# Return only some fields
gcx resources get dashboards --json metadata.name,spec.title

# Transform the output with a jq expression
gcx resources get dashboards --jq '.items | length'
```

When an agent doesn't use these flags, `gcx` prints a one-line hint to `stderr` that suggests them.

### Detect incomplete lists

List commands never truncate results silently.

For example, `gcx resources get` returns up to 50 items for each resource type by default, which keeps results small.

When a command returns partial results, it prints a hint to `stderr` with the command that returns more. Some list commands also add a `list_meta` object to the JSON output, so an agent can tell that the result is incomplete and which command to run next:

```json
"list_meta": {"truncated": true, "returned": 50, "continue": "gcx ... list --limit 100"}
```

### Handle large results

A large result can fill an agent's context window. When a result is larger than 100 KiB, `gcx` writes the full result to a temporary file named `gcx-results-*.json` and prints a short receipt to `stdout` instead:

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

The receipt includes a short `preview_sample`, never the complete result. The agent reads the file in `spilled_to` for the full data.

To avoid spilling, agents can pass `-o json` or redirect `stdout` to a file. To change the threshold, set [`GCX_AGENT_SPILL_BYTES`](../cli-reference/#gcx_agent_spill_bytes) in the agent's environment (`0` turns spilling off).

Spill files stay in the temporary directory. To remove the ones older than 30 minutes, agents can run [`gcx agent prune`](../cli-reference/#gcx-agent-prune), for example at the end of a session.

### Handle errors and exit codes

In agent mode, a failed command writes one JSON error document to `stdout`, and the `exitCode` field matches the process exit code:

```json
{"type": "gcx.error", "schema_version": "1", "error": {"summary": "Resource not found", "exitCode": 1}}
```

The `error` object can also include `details`, `suggestions`, and `docsLink`. When some commands, such as `gcx resources get`, partly succeed, the document has the type `gcx.partial_result` and includes the successful `items` alongside the `error`.

Agents use the process exit code to decide what to do next:

| Code | Meaning | What an agent can do |
| --- | --- | --- |
| `0` | Success | Continue. |
| `1` | General error | Read the error `summary` and `suggestions`. |
| `2` | Usage error, such as a bad flag or selector | Correct the command. |
| `3` | Authentication or authorization failure | Stop and ask you to sign in or change permissions. |
| `4` | Partial failure | Read the output to find the failed items, and retry them. |
| `5` | Cancelled | Ask you before it retries. |
| `6` | Grafana version not supported | Stop and report the version. |

Run `gcx help exit-codes` for descriptions and examples of each code.

## Configure agent mode

### Override agent mode detection

If `gcx` doesn't detect your tool automatically, set [`GCX_AGENT_NAME`](../cli-reference/#gcx_agent_name) to a supported label in the agent's command environment. For example, to identify **Crush**, run the following command:

```bash
GCX_AGENT_NAME=crush gcx resources get dashboards
```

Set the label only in the agent's environment, not in a shell that people also use. For details on how to set environment variables and how `gcx` uses them, refer to [Environment variables](../cli-reference/#environment-variables).


To turn agent mode on or off regardless of detection:

| To | Use |
| --- | --- |
| Turn agent mode on | `--agent` or `GCX_AGENT_MODE=1` |
| Turn agent mode off | `--agent=false` or `GCX_AGENT_MODE=0` |

The `--agent` flag takes precedence over environment variables. [`GCX_AGENT_MODE`](../cli-reference/#gcx_agent_mode) accepts `1`, `true`, or `yes` to turn agent mode on, and `0`, `false`, or `no` to turn it off.

### Log failed agent calls

To record failed agent-mode calls locally for debugging, enable `diagnostics.agent-invocation-log` in your [configuration](../configuration/#configuration-reference):

```bash
gcx config set diagnostics.agent-invocation-log true
```

When enabled, `gcx` writes failed agent-mode invocations (up to 1,000 JSONL records) to `~/.local/state/gcx/agent-invocation-errors.jsonl` (`$XDG_STATE_HOME/gcx/agent-invocation-errors.jsonl`), or to `diagnostics.log-dir` if configured.

## Learn more

- [Diagnose missing telemetry](../guides/diagnose-missing-telemetry/): An example workflow that uses an agent with `gcx` skills.
- [Usage statistics](../anonymous-usage-statistics/#agent-identity): What `gcx` reports about agent use, and how to opt out.
