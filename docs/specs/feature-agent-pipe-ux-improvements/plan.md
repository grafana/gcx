---
type: feature-plan
title: "Agent and Pipe UX Improvements"
status: draft
spec: docs/specs/feature-agent-pipe-ux-improvements/spec.md
created: 2026-03-09
---

# Architecture and Design Decisions

## Pipeline Architecture

The three features integrate at different layers of the existing command pipeline:

```
                          ┌──────────────────────────────────┐
                          │          main.go                 │
                          │  preParseAgentFlag()             │
                          │  handleError() ◄── T5: in-band  │
                          │    │              JSON errors    │
                          │    ▼                             │
                          │  root.Command()                  │
                          └──────────┬───────────────────────┘
                                     │
                          ┌──────────▼───────────────────────┐
                          │    root/command.go                │
                          │    PersistentPreRun               │
                          │  ┌─────────────────────────┐     │
                          │  │ T1: TTY detection       │     │
                          │  │ term.IsTerminal(stdout)  │     │
                          │  │   → set color.NoColor    │     │
                          │  │   → set IsPiped flag     │     │
                          │  └─────────────────────────┘     │
                          │  --no-color / NO_COLOR override  │
                          │  --no-truncate flag               │
                          └──────────┬───────────────────────┘
                                     │
                          ┌──────────▼───────────────────────┐
                          │    cmd/grafanactl/io/             │
                          │                                   │
                          │  Options struct                   │
                          │  ├── OutputFormat string          │
                          │  ├── IsPiped     bool  ◄── T2    │
                          │  ├── NoTruncate  bool  ◄── T2    │
                          │  ├── JSONFields  []string ◄── T3 │
                          │  └── customCodecs map             │
                          │                                   │
                          │  BindFlags():                     │
                          │    --json field1,field2 ◄── T3   │
                          │    mutual exclusion w/ -o ◄── T3 │
                          │                                   │
                          │  Encode():                        │
                          │    if JSONFields set → T4:       │
                          │      field-select codec           │
                          │    else → existing codec path    │
                          └──────────┬───────────────────────┘
                                     │
                   ┌─────────────────┼─────────────────┐
                   │                 │                  │
            ┌──────▼─────┐   ┌──────▼─────┐   ┌───────▼──────┐
            │ text codec  │   │ json codec │   │ field-select │
            │ (tables)    │   │ (builtin)  │   │ codec (new)  │
            │             │   │            │   │ T4           │
            │ reads       │   │            │   │ extracts     │
            │ IsPiped to  │   │            │   │ fields from  │
            │ skip trunc  │   │            │   │ unstructured │
            │ T2          │   │            │   │ objects      │
            └─────────────┘   └────────────┘   └──────────────┘
```

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Add `IsPiped` and `NoTruncate` fields to `cmd/grafanactl/io.Options` | The IO Options struct is the single point all commands use for output configuration. Table codecs already receive data through `Encode()` — they need access to pipe/truncation state via the codec or the writer context. Adding fields to Options and passing them to codecs keeps the pattern consistent. (FR-001, FR-003, FR-005) |
| Set `color.NoColor` in PersistentPreRun alongside existing `--no-color` logic | The `fatih/color` library already has a global `NoColor` bool. TTY detection simply adds another condition to the existing assignment. `NO_COLOR` is already handled by fatih/color internally. (FR-002, FR-004) |
| `--json` flag parsed as a comma-separated string in `io.Options.BindFlags()` | Follows the `gh` CLI convention. The sentinel value `?` triggers field discovery. Parsing happens in the same `BindFlags()` call that handles `-o`, making mutual exclusion validation straightforward in `Validate()`. (FR-006, FR-009) |
| Field-selection codec wraps the existing JSON codec | Rather than modifying the built-in JSON codec, a new `FieldSelectCodec` accepts the field list and delegates serialization to `format.JSONCodec`. This preserves backward compatibility and keeps the field extraction logic isolated. (FR-006, FR-008) |
| `--json ?` uses discovery registry to enumerate resource fields | The discovery registry already resolves GVK from selectors. For field discovery, we fetch a single resource instance and enumerate its top-level + `spec.*` keys. This avoids maintaining a static schema catalog. (FR-007) |
| In-band error JSON written in `handleError()` in `main.go` | `handleError()` is the single error exit point. Adding JSON output here (conditional on agent mode OR `--json` flag) means every command gets in-band errors without per-command changes. The `DetailedError` struct already has all needed fields (Summary, Details, Suggestions, DocsLink, ExitCode). (FR-011, FR-011a, FR-014) |
| Error envelope uses `{"error": {...}, "items": [...]}` shape | For partial failures (FR-012), the command writes results normally, then `handleError()` wraps the output in an envelope. For commands that already wrote output, we use a structured wrapper that commands opt into for batch operations. |
| `--no-truncate` as explicit flag rather than auto-detect only | Some users want untruncated output on a TTY (e.g., wide terminal with small data). The flag provides explicit control beyond auto-detection. (FR-005) |
| Agent mode implies all pipe-aware behaviors | When `agent.IsAgentMode()` is true, auto-set `IsPiped=true`, `NoTruncate=true`, `color.NoColor=true`. Agents always want clean output regardless of TTY state. This consolidates the existing agent→color logic with the new pipe-aware logic. (FR-005a) |

## Compatibility

**Unchanged behavior:**
- `--no-color` flag continues to work identically
- `-o json`, `-o yaml`, `-o text`, `-o wide` flags unchanged
- `--agent` flag and agent mode env var detection unchanged
- All existing table codecs continue to produce the same output when stdout is a TTY
- All custom codecs registered via `RegisterCustomCodec()` continue to work

**New behavior (additive):**
- Non-TTY stdout automatically disables color and truncation
- `--json field1,field2` produces filtered JSON output
- `--json ?` prints available fields and exits
- Agent mode errors include JSON on stdout in addition to stderr
- `--no-truncate` flag available on all commands

**Deprecations:** None.
