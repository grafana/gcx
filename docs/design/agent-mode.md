# Agent Mode

> Covers agent mode detection via environment variables and --agent flag, behavior changes when active, opt-out mechanisms, and exempt commands.

---

## 6. Agent Mode

### 6.1 Detection

Agent mode is detected via environment variables at `init()` time in
`internal/agent/agent.go` and via the `--agent` CLI flag pre-parsed in
`main.go` before Cobra command construction.

| Variable | Set by | Effect |
|----------|--------|--------|
| `GCX_AGENT_MODE` | Explicit opt-in/out | `1`/`true`/`yes` enables; `0`/`false`/`no` **disables** (overrides all others) |
| `CLAUDECODE` | Claude Code | Truthy value activates agent mode |
| `CLAUDE_CODE` | Claude Code | Truthy value activates agent mode |
| `CURSOR_AGENT` | Cursor | Truthy value activates agent mode |
| `GITHUB_COPILOT` | GitHub Copilot | Truthy value activates agent mode |
| `AMAZON_Q` | Amazon Q | Truthy value activates agent mode |

The `--agent` persistent flag can also enable agent mode. `--agent=false`
explicitly disables agent mode even when env vars are set.

**Priority order:** `GCX_AGENT_MODE=0` (disable) > any truthy env var
(enable) > `--agent` flag > default (disabled).

**API:** `agent.IsAgentMode() bool`, `agent.SetFlag(bool)`, `agent.DetectedFromEnv() bool`

Reference: `internal/agent/agent.go`

### 6.2 Behavior Changes

When agent mode is active:
1. **Default output format** becomes `json` for all commands (overrides
   per-command `DefaultFormat()` in `io.Options.BindFlags()`)
2. **Color** is disabled (`color.NoColor = true` in `PersistentPreRun`)
3. **Pipe-aware behavior** is forced: `IsPiped=true`, `NoTruncate=true`
   regardless of actual TTY state (see [pipe-awareness.md § TTY Detection](pipe-awareness.md#51-tty-detection))
4. **In-band error JSON** is written to stdout on failure (see [errors.md § In-Band Error Reporting](errors.md#44-in-band-error-reporting))

The following are **not yet implemented**:
5. Spinners/progress indicators suppressed (none exist yet; the suppression
   contract via `IsPiped` is in place for when they are added)
6. Confirmation prompts auto-approved ([safety.md § Agent Mode Auto-Approve](safety.md#33-agent-mode-auto-approve))

**Note:** The `--json list` hint banner has been removed and is no longer emitted in any mode
(agent or non-agent). Previously the banner was only emitted in agent mode; it has been
deleted entirely.

### 6.2a Format choice vs non-format presentation properties

**Format choice** (`-o text/wide/json/yaml`) is controlled by explicit flags. An explicit `-o wide` overrides the agent-mode JSON default — this is documented behavior.

**Non-format presentation properties** (color, truncation, box-drawing characters) are ALWAYS suppressed in agent mode, regardless of which format is active:
- `-o wide` under agent mode: renders a wide table with no ANSI colors, no box chars.
- `-o json` under agent mode: JSON output with no box characters in any string field.

### 6.3 Opt-Out

Explicit flags override agent mode defaults:
- `-o text` or `-o yaml` overrides the JSON default
- `-o wide` retains human table output even in agent mode (explicit-override semantics — the
  operator has explicitly requested wide table format, so the JSON default is not applied)
- `--agent=false` disables agent mode entirely (even when env vars are set)
- `GCX_AGENT_MODE=0` disables agent mode regardless of other env vars

### 6.4 Exempt Commands

Commands that produce non-data output are exempt from format switching:
- `config set`, `config use-context` — confirmations only
- `serve` — starts a long-running server
- `push`, `pull` — output is status messages, not data

See [environment-variables.md § Agent Mode Variables](environment-variables.md#agent-mode-variables) for the full variable reference.
