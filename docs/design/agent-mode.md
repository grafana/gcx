# Agent Mode

> Covers agent mode detection via environment variables and --agent flag, behavior changes when active, opt-out mechanisms, and exempt commands.
> Status markers: **[CURRENT]** = enforced, **[ADOPT]** = new code must follow, **[PLANNED]** = future.

---

## 6. Agent Mode

### 6.1 Detection `[CURRENT]`

Agent mode is detected via environment variables at `init()` time in
`internal/agent/agent.go` and via the `--agent` CLI flag pre-parsed in
`main.go` before Cobra command construction.

| Variable | Set by | Effect |
|----------|--------|--------|
| `GCX_AGENT_MODE` | Explicit opt-in/out | `1`/`true`/`yes` enables; `0`/`false`/`no` **disables** (overrides all others) |
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

### 6.2 Behavior Changes `[CURRENT]`

When agent mode is active:
1. **Default output format** becomes `json` for all commands (overrides
   per-command `DefaultFormat()` in `io.Options.BindFlags()`)
2. **Color** is disabled (`color.NoColor = true` in `PersistentPreRun`)
3. **Pipe-aware behavior** is forced: `IsPiped=true`, `NoTruncate=true`
   regardless of actual TTY state (see [pipe-awareness.md § TTY Detection](pipe-awareness.md#51-tty-detection))
4. **In-band error JSON** is written to stdout on failure (see [errors.md § In-Band Error Reporting](errors.md#44-in-band-error-reporting))

The following are **not yet implemented** (`[PLANNED]`):
5. Spinners/progress indicators suppressed (none exist yet; the suppression
   contract via `IsPiped` is in place for when they are added)
6. Confirmation prompts auto-approved ([safety.md § Agent Mode Auto-Approve](safety.md#33-agent-mode-auto-approve))

### 6.3 Opt-Out `[CURRENT]`

Explicit flags override agent mode defaults:
- `-o text` or `-o yaml` overrides the JSON default
- `--agent=false` disables agent mode entirely (even when env vars are set)
- `GCX_AGENT_MODE=0` disables agent mode regardless of other env vars

### 6.4 Exempt Commands `[PLANNED]`

Commands that produce non-data output are exempt from format switching:
- `config set`, `config use-context` — confirmations only
- `serve` — starts a long-running server
- `push`, `pull` — output is status messages, not data

See [environment-variables.md § Agent Mode Variables](environment-variables.md#agent-mode-variables-current) for the full variable reference.
