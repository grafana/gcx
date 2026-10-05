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
| `OPENCODE` | opencode | Truthy value activates agent mode |
| `PI_CODING_AGENT` | pi | Truthy value activates agent mode |

The `--agent` persistent flag can also enable agent mode. `--agent=false`
explicitly disables agent mode even when env vars are set.

**Priority order:** `GCX_AGENT_MODE=0` (disable) > any truthy env var
(enable) > `--agent` flag > default (disabled).

**API:** `agent.IsAgentMode() bool`, `agent.SetFlag(bool)`, `agent.DetectedFromEnv() bool`

Reference: `internal/agent/agent.go`

### 6.2 Behavior Changes

When agent mode is active:
1. **Default output format** becomes `agents` for all commands (overrides
   per-command `DefaultFormat()` in `io.Options.BindFlags()`). The `agents`
   codec emits compact JSON when the payload is ≤ 100 KiB and spills to a
   temp file otherwise — see [output.md § Agents Codec](output.md#111-agents-codec)
2. **Color** is disabled (`color.NoColor = true` in `PersistentPreRun`)
3. **Pipe-aware behavior** is forced: `IsPiped=true`, `NoTruncate=true`
   regardless of actual TTY state (see [pipe-awareness.md § TTY Detection](pipe-awareness.md#51-tty-detection))
4. **In-band error JSON** is written to stdout on failure (see [errors.md § In-Band Error Reporting](errors.md#44-in-band-error-reporting))

The following are **not yet implemented**:
5. Spinners/progress indicators suppressed (none exist yet; the suppression
   contract via `IsPiped` is in place for when they are added)
6. Confirmation prompts auto-approved ([safety.md § Agent Mode Auto-Approve](safety.md#33-agent-mode-auto-approve))

**Agent-mode field-selection hint.** gcx shows this hint only when it can
help and cannot break the output. The hint text is:

```
{"class":"hint","summary":"use --json list / --json field1,field2 for field selection, or --jq '<expr>' for transformation (group_by, filter, count) — no external parsing needed"}
```

gcx shows the hint when all of these conditions are true:

1. Agent mode is active.
2. The codec is JSON-like (`agents` or `json`).
3. The command does not use `--json` (field selection or `--json list`).
4. The command does not use `--jq`.
5. The command does not pin its default format (file-output commands).
6. The encoded payload is 8 KiB or larger (`fieldsHintMinBytes` in
   `internal/output/format.go`).

Where the hint goes:

- **Payload on stdout:** one JSONL `class:"hint"` record on stderr, after the
  payload.
- **Payload spilled to a file:** the `hint` field of the spill receipt on
  stdout. stderr stays empty.

The hint appears no more than one time for each `Options` value
(`jsonFieldsHintShown` guard). It never appears outside agent mode.

**Why the rule is narrow.** Agents often merge the two streams
(`gcx ... 2>&1 | jq ...`). An earlier version of gcx wrote the hint for
almost each JSON result, also when `--json` fields were selected. The hint
was then the first line of the merged stream, and `jq` failed
(`jq: error (at <stdin>:1)`), or the agent used a turn to remove the line
(`sed 1d`, `tail -n +2`). A token-efficiency eval found this problem in 31 of
82 analyzed trials. A small payload costs few tokens, so the hint gives no
value there. A command with `--json` fields already has the selection, so
the hint gives no value there either.

**Spill receipts in agent mode** do not write a stderr hint. The receipt on
stdout already names the file in `spilled_to` and `message`. Outside agent
mode, an explicit `-o agents` spill still writes a `hint: ...` line to stderr.

### 6.2a Format choice vs non-format presentation properties

**Format choice** (`-o text/wide/json/yaml`) is controlled by explicit flags. An explicit `-o wide` overrides the agent-mode JSON default — this is documented behavior.

**Non-format presentation properties** (color, truncation, box-drawing characters) are ALWAYS suppressed in agent mode, regardless of which format is active:
- `-o wide` under agent mode: renders a wide table with no ANSI colors, no box chars.
- `-o json` under agent mode: JSON output with no box characters in any string field.

### 6.3 Opt-Out

Explicit flags override agent mode defaults:
- `-o json` forces full indented JSON to stdout (no spill)
- Bare `--jq` disables spill; add `-o agents` for
  [compact/spill handling](output.md#16-jq-transformation)
- `-o text` or `-o yaml` overrides the agents default
- `-o wide` retains human table output even in agent mode (explicit-override semantics — the
  operator has explicitly requested wide table format, so the JSON default is not applied)
- `--agent=false` disables agent mode entirely (even when env vars are set)
- `GCX_AGENT_MODE=0` disables agent mode regardless of other env vars
- `GCX_AGENT_SPILL_BYTES=<n>` adjusts the spill threshold (bytes; default 102400)

### 6.4 Output Protocol Classes

Every runnable leaf command declares an output protocol class in
`cmd/gcx/root/testdata/output_classes.json`, enforced by
`TestConsistency_AllLeafCommandsHaveOutputClass` — a new command cannot
land unclassified. When agent mode supplies the default (no explicit
`-o`/`--json`/`--jq`):

| Class | Agent-mode stdout contract |
|-------|---------------------------|
| `finite` | Exactly one JSON value — the result, or a fused/in-band error document — with the process exit code agreeing with the outcome. A command that has already written its complete document returns `gcxerrors.EmittedError` so the reporter never appends a second one. |
| `artifact` | Files on disk are the real output; stdout carries exactly one JSON receipt (`gcx.artifact_receipt`: paths, format, counts, failures). Applies to the pull family (`resources pull`, `slo definitions/reports pull`). The `-o` flag selects the FILE format and is pinned via `Options.PinDefaultFormat` — agent mode must never produce `.agents` resource files or spill envelopes as manifests (`resources edit` shares the pin). Commands that write files as a side effect but answer with an ordinary result document (skills install, dev generate, config set) are class `finite`. |
| `stream` | Typed, versioned JSONL: every line independently parseable with a `type` discriminator, ending in a terminal success/error event. |
| `interactive` | Drives a prompt, editor, or wizard — exempt from the JSON contract, but must never block in agent mode: confirmation gates fail fast without `--force` (`CheckDestructiveBypass`), approval prompts are explicitly declined, and `resources edit` fails with an instructive error when no `EDITOR`/`VISUAL` is configured (an explicitly configured editor is honored — non-interactive editors are legitimate automation). Known gap: browser-OAuth login still blocks on its localhost callback in agent mode; use token auth in harnesses (follow-up). |
| `server` / `shell` / `prose` / `raw` | Long-running listeners, completion scripts, help prose, and byte passthrough (`gcx api`, alert exports, kubectl-pipeable YAML emitters) — exempt, declared. |

Explicit protocol-changing flags follow the flag, not the class: `--open`
(browser deep link; in agent mode the URL arrives as a typed stderr hint and
stdout may be empty), `-o pprof`/`-o raw` (binary/raw artifacts), and
`--json list`/`--json ?` (plain field-per-line discovery output) are all
explicit requests that override the declared default protocol.

The conformance suite (`cmd/gcx/root/agentconformance_test.go`) builds a
fresh binary and pins the finite contract end-to-end: one JSON value then
EOF, in-band `gcx.error` document on failure with agreeing exit code,
explicit `-o` overrides honored, stdin closed.

See [environment-variables.md § Agent Mode Variables](environment-variables.md#agent-mode-variables) for the full variable reference.
