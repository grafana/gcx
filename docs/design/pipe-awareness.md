# Pipe-Awareness

> Describes TTY detection, automatic pipe behavior (including the non-TTY NDJSON output default), --no-color, and NO_COLOR environment variable support.

---

## 5. Pipe-Awareness

### 5.1 TTY Detection

Root `PersistentPreRun` calls `terminal.Detect()` which uses
`term.IsTerminal(os.Stdout.Fd())` to determine whether stdout is connected to
a terminal. The result is stored as package-level state in `internal/terminal`.

**Automatic behaviors when stdout is piped (not a TTY):**
- Color is disabled (`color.NoColor = true`)
- Table column truncation is suppressed (`NoTruncate = true`)
- **Default output format becomes `ndjson`** (unless an explicit `-o` is given) —
  one JSON object per line, so a `2>&1`-merged stream stays parseable. Resolved
  in `io.Options.Validate()`, not `PersistentPreRun`. See
  [output.md § NDJSON Codec](output.md#112-ndjson-codec). Diagnostics on stderr
  (`hint`/`warning`/`note`) and the stdout error envelope (`error`) are likewise
  emitted as `kind`-tagged JSON lines when stdout is non-TTY.

**Override flags** (available on all commands):
- `--no-truncate` — explicitly disables truncation regardless of TTY state
- `--no-color` — explicitly disables color regardless of TTY state

**Agent mode implies pipe behavior** (FR-005a): when `agent.IsAgentMode()` is
true, `terminal.SetPiped(true)` and `terminal.SetNoTruncate(true)` are set
regardless of actual TTY state. Agents always want clean, machine-parseable
output.

**Detection order in `PersistentPreRun`:**

```
1. terminal.Detect()            ← TTY auto-detection
2. agent mode → SetPiped(true)  ← agent mode overrides
3. --no-truncate → SetNoTruncate(true)  ← explicit flag wins
4. --no-color or IsPiped → color.NoColor = true
```

**Note on CI environments:** Some CI runners (e.g. GitHub Actions) may report
stdout as a TTY. Use `--no-color` and `--no-truncate` for reliable override in
automated pipelines.

**Implementation:** `internal/terminal/terminal.go` (`Detect`, `IsPiped`,
`NoTruncate`, `SetPiped`, `SetNoTruncate`). Invoked from
`cmd/gcx/root/command.go` (`PersistentPreRun`).

Codecs read `terminal.IsPiped()` and `terminal.NoTruncate()` directly at encode
time. (`io.Options.IsPiped`/`NoTruncate` are also populated in `BindFlags`, but
note `BindFlags` runs at command-construction time — before `terminal.Detect()`
in `PersistentPreRun` — so the pipe-dependent output default is resolved later,
in `io.Options.Validate()`, which runs inside `RunE`.) Table codecs use
`NoTruncate` to skip ellipsis truncation.

### 5.2 `--no-color` Flag

Implemented in `cmd/gcx/root/command.go`. Sets `color.NoColor = true`
globally. Takes precedence over TTY auto-detection — passing `--no-color` on
a TTY still disables color.

### 5.3 `NO_COLOR` Environment Variable

The [no-color.org](https://no-color.org/) convention. The `fatih/color`
library already checks `NO_COLOR` automatically, so this works today. Document
it in help text and env var references so users know it's available.

### 5.4 Auto-Format Switching

Future consideration: when piped and no explicit `-o` flag, commands with
`text` default could auto-switch to a more parseable format (e.g. JSON or
tab-separated). Needs design discussion.

Reference: `cmd/gcx/root/command.go` (`PersistentPreRun`)

See [agent-mode.md](agent-mode.md) for how agent mode interacts with pipe behavior.
See [environment-variables.md](environment-variables.md) for the `NO_COLOR` variable reference.
