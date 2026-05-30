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
1. **Default output format** becomes `ndjson` for all commands. Agent mode forces
   pipe-aware behavior (`IsPiped=true`), and the non-TTY default is NDJSON —
   resolved in `io.Options.Validate()`, overriding the per-command
   `DefaultFormat()`. Each data line is wrapped `{"kind":"result","data":...}`;
   oversized output still spills to a temp file (one `{"kind":"spill",...}` line).
   See [output.md § NDJSON Codec](output.md#112-ndjson-codec). The single-object
   `agents` codec remains available via explicit `-o agents`. NDJSON trades the
   `agents` codec's maximal compactness (one document) for line-oriented
   robustness: a `2>&1`-merged stream stays parseable, and large lists stream
   line-by-line; spill still bounds truly oversized payloads.
2. **Color** is disabled (`color.NoColor = true` in `PersistentPreRun`)
3. **Pipe-aware behavior** is forced: `IsPiped=true`, `NoTruncate=true`
   regardless of actual TTY state (see [pipe-awareness.md § TTY Detection](pipe-awareness.md#51-tty-detection))
4. **In-band error JSON** is written to stdout on failure (see [errors.md § In-Band Error Reporting](errors.md#44-in-band-error-reporting))

The following are **not yet implemented**:
5. Spinners/progress indicators suppressed (none exist yet; the suppression
   contract via `IsPiped` is in place for when they are added)
6. Confirmation prompts auto-approved ([safety.md § Agent Mode Auto-Approve](safety.md#33-agent-mode-auto-approve))

**Note:** The `--json list` field-discovery hint fires whenever the resolved output codec
is JSON-like (`-o json`, the `ndjson` non-TTY default, or `-o agents`) and the caller has
not already used `--json list` (field discovery) or `--json field1,field2` (field selection).
When stdout is non-TTY (pipe or agent mode) the hint is emitted as JSONL
`{"kind":"hint","summary":"..."}` on stderr. In TTY mode it is emitted as `hint: ...` text on stderr. The
hint is emitted at most once per invocation.

### 6.2a Format choice vs non-format presentation properties

**Format choice** (`-o text/wide/json/yaml`) is controlled by explicit flags. An explicit `-o wide` overrides the agent-mode NDJSON default — this is documented behavior.

**Non-format presentation properties** (color, truncation, box-drawing characters) are ALWAYS suppressed in agent mode, regardless of which format is active:
- `-o wide` under agent mode: renders a wide table with no ANSI colors, no box chars.
- `-o json` under agent mode: JSON output with no box characters in any string field.

### 6.3 Opt-Out

Explicit flags override agent mode defaults:
- `-o json` forces a single pretty JSON document to stdout (no NDJSON line-wrapping)
- `-o agents` forces the single-object compact-JSON-with-spill codec
- `-o text` or `-o yaml` overrides the ndjson default
- `-o wide` retains human table output even in agent mode (explicit-override semantics — the
  operator has explicitly requested wide table format, so the NDJSON default is not applied)
- `--agent=false` disables agent mode entirely (even when env vars are set)
- `GCX_AGENT_MODE=0` disables agent mode regardless of other env vars
- `GCX_AGENT_SPILL_BYTES=<n>` adjusts the spill threshold (bytes; default 102400)

### 6.4 Exempt Commands

Commands that produce non-data output are exempt from format switching:
- `config set`, `config use-context` — confirmations only
- `serve` — starts a long-running server
- `push`, `pull` — output is status messages, not data

See [environment-variables.md § Agent Mode Variables](environment-variables.md#agent-mode-variables) for the full variable reference.
