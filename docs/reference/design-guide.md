# Design Guide: Command and Provider UX

> Prescriptive UX requirements for anyone building new commands or providers.
> Read this before implementing features. Reference alongside [cli-layer.md](../architecture/cli-layer.md)
> for command structure and [patterns.md](../architecture/patterns.md) for architectural patterns.

## Status Markers

Each subsection is tagged with an implementation status:

- **`[CURRENT]`** — Implemented and enforced. Follow exactly.
- **`[ADOPT]`** — Not consistently applied yet. **New code MUST follow this.** If
  modifying existing code in this area, adopt the pattern while you're there.
- **`[PLANNED]`** — Future infrastructure. Documented for context; do not implement
  piecemeal unless the tracking issue explicitly asks for it.

New commands and providers **must comply with all `[CURRENT]` and `[ADOPT]` items**.

---

## 1. Output Contract

### 1.1 Built-in Codecs `[CURRENT]`

Every command gets `json` and `yaml` output for free via `io.Options`. These
produce the full resource object as returned by the API — no envelope wrapping,
no field filtering. This output is stable.

```go
ioOpts := &io.Options{}
ioOpts.BindFlags(cmd.Flags())
```

### 1.2 Custom Codecs `[CURRENT]`

Commands register additional formats (e.g. `text`, `wide`, `graph`) via
`io.Options.RegisterCustomCodec()`. The `text` codec is a Kubernetes-style
table printer (`k8s.io/cli-runtime/pkg/printers.NewTablePrinter`).

```go
ioOpts.RegisterCustomCodec("text", myTableCodec)
ioOpts.DefaultFormat("text")   // makes "text" the default instead of "json"
```

**Data fetching is format-agnostic.** Commands must fetch all available data
in `RunE` regardless of the `--output` value. The output format controls
**presentation**, not **data acquisition**. Table/wide codecs select which
columns to render; the built-in JSON/YAML codecs serialize the full data
structure. Do not gate data fetches on `opts.IO.OutputFormat` — this causes
JSON/YAML to silently omit fields. See Pattern 13 in `patterns.md`.

### 1.3 Default Format by Command Type `[ADOPT]`

| Command type | Default format | Rationale |
|-------------|---------------|-----------|
| `list`, `get` | `text` (with table codec) | Human-scannable |
| `config view` | `yaml` | Config is YAML-native |
| `push`, `pull`, `delete` | Status messages only | Operations, not data |
| Agent mode (Section 6) | `json` | Machine-parseable |

When building a new command: call `ioOpts.DefaultFormat("text")` for data
display commands and register a table codec. Don't leave `json` as the default
for interactive commands.

### 1.4 Status Messages `[CURRENT]`

Use the `cmdio` functions for operation feedback — they use Unicode symbols
and respect `color.NoColor`:

```go
cmdio.Success(cmd.OutOrStdout(), "Pushed %d resources", count)  // ✔
cmdio.Warning(cmd.OutOrStdout(), "Skipped %d resources", count) // ⚠
cmdio.Error(cmd.OutOrStdout(), "Failed %d resources", count)    // ✘
cmdio.Info(cmd.OutOrStdout(), "Using context %q", ctx)          // 🛈
```

Status messages go to stdout. Errors (via `DetailedError`) go to stderr.

Reference: `internal/output/messages.go`

### 1.5 JSON Field Selection `[CURRENT]`

The `--json` flag selects specific fields from output objects. When provided,
output is always JSON regardless of the `--output` default.

```bash
# Select specific fields from a single resource
gcx resources get dashboards/my-dash --json metadata.name,spec.title

# List operation: output is {"items": [...]}
gcx resources get dashboards --json metadata.name

# Discover available field paths for a resource type
gcx resources get dashboards/my-dash --json ?
```

**Flag semantics:**

| Value | Behavior |
|-------|----------|
| `--json field1,field2` | Emit JSON with only those fields; missing fields produce `null` |
| `--json ?` | Print available field paths (one per line, sorted) and exit 0 |
| `--json` + `-o json` | Allowed — both request JSON, no conflict |
| `--json` + `-o <non-json>` | Usage error — field selection requires JSON output |

**Field path syntax:** Dot-notation resolves nested fields. `metadata.name`
extracts `metadata → name`. Top-level keys and `spec.*` sub-keys are enumerated
by `--json ?`. Field discovery introspects a sample object from the API — no
additional list calls are made (NC-005).

**Output shape:**
- Single resource: `{"field": "value", ...}` (flat object, only selected fields)
- List/collection: `{"items": [{"field": "value"}, ...]}`

**Backward compatibility:** `-o json` is unchanged — it still produces the full
resource object. `--json` is an independent mechanism (NC-002).

**Implementation:** `internal/output/field_select.go` (`FieldSelectCodec`,
`DiscoverFields`). Flag parsing and mutual-exclusion enforcement in
`internal/output/format.go` (`applyJSONFlag`).

---

## 2. Exit Code Taxonomy

### 2.1 Exit Codes `[CURRENT]`

| Code | Constant | Meaning | When |
|------|----------|---------|------|
| 0 | `ExitSuccess` | Success | Command completed without errors |
| 1 | `ExitGeneralError` | General error | Unexpected error, business logic failure |
| 2 | `ExitUsageError` | Usage error | Bad flags, invalid selectors, missing args `[RESERVED]` |
| 3 | `ExitAuthFailure` | Auth failure | 401/403, missing or invalid credentials |
| 4 | `ExitPartialFailure` | Partial failure | Some resources succeeded, others failed `[RESERVED]` |
| 5 | `ExitCancelled` | Cancelled | User pressed Ctrl+C (SIGINT) or `context.Canceled` |
| 6 | `ExitVersionIncompatible` | Version incompatible | Grafana version < 12 detected |

Constants defined in `cmd/gcx/fail/exitcodes.go`.

**Implementation state:**
- Exit code 3 (auth failure) is set by `convertAPIErrors` for HTTP 401/403.
- Exit code 5 (cancelled) is set by `convertContextCanceled` (first in converter
  chain) and by a fast-path check in `handleError` for `context.Canceled`.
- SIGINT is handled via `signal.NotifyContext` in `main.go`, which cancels the
  context and produces exit code 5.
- Exit codes 2, 4, and 6 are defined as constants but not yet wired to converters.

### 2.2 Setting Exit Codes in Converters `[ADOPT]`

When writing or modifying error converters in `cmd/gcx/fail/convert.go`,
set the `ExitCode` field on `DetailedError`:

```go
// In convertAPIErrors, for auth failures:
exitCode := 3
return &DetailedError{
    Summary:  fmt.Sprintf("%s - code %d", reason, code),
    ExitCode: &exitCode,
    Suggestions: []string{...},
}, true
```

For partial failures, the command itself should set exit code 4 when
`OperationSummary.FailedCount() > 0`.

### 2.3 Cobra Usage Errors `[CURRENT]`

Cobra itself handles usage errors (bad flags, missing required args). With
`SilenceUsage: true` set on the root command, these errors flow through
`handleError` and get exit code 1. Future work: detect Cobra usage errors
and override to code 2.

Reference: `cmd/gcx/main.go`, `cmd/gcx/fail/detailed.go`,
`cmd/gcx/fail/convert.go`

---

## 3. Confirmation and Safety

### 3.1 When to Prompt `[ADOPT]`

Prompt the user before:
- Deleting remote resources (single or bulk)
- Bulk overwrite operations (`push --overwrite` on an existing resource set)

Do NOT prompt for:
- Push (create-or-update) — it's idempotent
- Pull (local write) — easily reversible via git
- Config changes — low-risk, undoable

### 3.2 The `--yes` / `-y` Pattern `[IMPLEMENTED]`

The `--yes`/`-y` flag and `GCX_AUTO_APPROVE` environment variable enable
non-interactive operation for destructive commands. Currently implemented for:

- **delete command**: Auto-enables `--force` flag (required to delete all resources of a type)

**Note:** Auto-approval does NOT enable `--include-managed` to protect resources
managed by external tools (Terraform, GitSync, etc.). Users must explicitly pass
`--include-managed` if needed.

Pattern (as implemented in `cmd/gcx/resources/delete.go`):

```go
// Load CLI options from environment
cliOpts, err := config.LoadCLIOptions()
if err != nil {
    return err
}

// Apply auto-approval logic
if (opts.Yes || cliOpts.AutoApprove) && !opts.Force {
    cmdio.Info(cmd.OutOrStdout(), "Auto-approval enabled: automatically setting --force")
    opts.Force = true
}
```

**Flag precedence:** Explicit flag value > --yes flag > env var > default

### 3.3 Agent Mode Auto-Approve `[PLANNED]`

When agent mode is active (Section 7), prompts are auto-approved. Agents
cannot interact with TTY prompts.

### 3.4 Dry-Run `[CURRENT]`

`--dry-run` is available on `push` and `delete`. It passes
`DryRun: []string{"All"}` to Kubernetes API options. Always document dry-run
support in new commands that modify remote state.

### 3.5 Push Idempotency `[CURRENT]`

Push is **idempotent** (create-or-update). The flow: Get → if exists: Update
with `resourceVersion`, if 404: Create. Safe to run repeatedly with the same
input. Document this explicitly in push-like commands:

```
# Push is idempotent: creates new resources and updates existing ones
gcx resources push ./dashboards/
```

Reference: `data-flows.md` Section 2 (PUSH Pipeline)

---

## 4. Error Design

### 4.1 DetailedError Structure `[CURRENT]`

All errors rendered to users pass through `DetailedError`:

```go
type DetailedError struct {
    Summary     string      // Required — one-liner describing what went wrong
    Details     string      // Optional — additional context
    Parent      error       // Optional — underlying error
    Suggestions []string    // Optional — actionable fixes
    DocsLink    string      // Optional — link to documentation
    ExitCode    *int        // Optional — override exit code (default: 1)
}
```

Rendering format (stderr, colored):
```
Error: File not found
│
│ could not read './dashboards/foo.yaml'
│
├─ Suggestions:
│
│ • Check for typos in the command's arguments
│
└─
```

Reference: `cmd/gcx/fail/detailed.go`

### 4.2 Writing Good Suggestions `[ADOPT]`

Every `DetailedError` **should** include at least one actionable suggestion.
Suggestions must be commands the user can run — not vague advice:

```go
// Good:
Suggestions: []string{
    "Review your configuration: gcx config view",
    "Set your token: gcx config set contexts.<ctx>.grafana.token <value>",
}

// Bad:
Suggestions: []string{
    "Check your configuration",
    "Make sure things are set up correctly",
}
```

### 4.3 Error Converter Extension `[CURRENT]`

Add new error types by implementing a converter function and appending to
`errorConverters` in `cmd/gcx/fail/convert.go`:

```go
func convertMyErrors(err error) (*DetailedError, bool) {
    var myErr *mypackage.SpecificError
    if !errors.As(err, &myErr) {
        return nil, false
    }
    return &DetailedError{
        Summary:     "Descriptive summary",
        Parent:      err,
        Suggestions: []string{"gcx ..."},
    }, true
}
```

Converters are tried in order — first match wins. Place more specific
converters before more general ones.

### 4.4 In-Band Error Reporting `[CURRENT]`

When agent mode is active and a command fails, a JSON error object is written
to **stdout** in addition to the existing stderr `DetailedError` output
(NC-003 — in-band JSON is additive, not a replacement).

**Error-only response** (command fails completely):

```json
{"error": {"summary": "Resource not found - code 404", "exitCode": 1}}
```

**Partial failure** (batch operation, some resources succeeded):

```json
{
  "items": [...],
  "error": {"summary": "3 resources failed", "exitCode": 4, "details": "...", "suggestions": ["..."]}
}
```

**JSON schema** (`error` object):

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `summary` | string | yes | One-liner from `DetailedError.Summary` |
| `exitCode` | int | yes | Matches the process exit code |
| `details` | string | no | Omitted when empty |
| `suggestions` | []string | no | Omitted when empty |
| `docsLink` | string | no | Omitted when empty |

**Guarantees:**
- On success, no `error` key appears in stdout JSON (NC-004).
- When agent mode is NOT active, no error JSON is written to stdout.
- The JSON is always valid — partial writes cannot corrupt it (NC-004).

**Implementation:** `cmd/gcx/fail/json.go` (`DetailedError.WriteJSON`).
Invoked from `handleError` in `cmd/gcx/main.go` when `agent.IsAgentMode()` is true.

---

## 5. Pipe-Awareness

### 5.1 TTY Detection `[CURRENT]`

Root `PersistentPreRun` calls `terminal.Detect()` which uses
`term.IsTerminal(os.Stdout.Fd())` to determine whether stdout is connected to
a terminal. The result is stored as package-level state in `internal/terminal`.

**Automatic behaviors when stdout is piped (not a TTY):**
- Color is disabled (`color.NoColor = true`)
- Table column truncation is suppressed (`NoTruncate = true`)

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

Codecs read `terminal.IsPiped()` and `terminal.NoTruncate()` at encode time
(via `io.Options.IsPiped` and `io.Options.NoTruncate` populated during
`BindFlags`). Table codecs use `NoTruncate` to skip ellipsis truncation.

### 5.2 `--no-color` Flag `[CURRENT]`

Implemented in `cmd/gcx/root/command.go`. Sets `color.NoColor = true`
globally. Takes precedence over TTY auto-detection — passing `--no-color` on
a TTY still disables color.

### 5.3 `NO_COLOR` Environment Variable `[ADOPT]`

The [no-color.org](https://no-color.org/) convention. The `fatih/color`
library already checks `NO_COLOR` automatically, so this works today. Document
it in help text and env var references so users know it's available.

### 5.4 Auto-Format Switching `[PLANNED]`

Future consideration: when piped and no explicit `-o` flag, commands with
`text` default could auto-switch to a more parseable format (e.g. JSON or
tab-separated). Needs design discussion.

Reference: `cmd/gcx/root/command.go` (`PersistentPreRun`)

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
   regardless of actual TTY state (see Section 5.1)
4. **In-band error JSON** is written to stdout on failure (see Section 4.4)

The following are **not yet implemented** (`[PLANNED]`):
5. Spinners/progress indicators suppressed (none exist yet; the suppression
   contract via `IsPiped` is in place for when they are added)
6. Confirmation prompts auto-approved (Section 3.3)

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

---

## 7. Provider Command Checklist

Extends the interface checklist in [provider-guide.md](provider-guide.md) with
UX requirements. All items are `[ADOPT]` unless marked otherwise.

### Interface Compliance `[CURRENT]`

- [ ] Struct implements all five `Provider` interface methods
- [ ] `Name()` is lowercase, unique, and stable (it's the config map key)
- [ ] All config keys are declared in `ConfigKeys()`
- [ ] Secret keys (passwords, tokens, API keys) have `Secret: true`
- [ ] `Validate()` returns error pointing to `gcx config set ...`
- [ ] Provider added to `internal/providers/registry.go:All()`

### UX Compliance `[ADOPT]`

- [ ] All data-display commands support `-o json/yaml` (inherited from `io.Options`)
- [ ] List/get commands register a `text` table codec as default format
- [ ] List/get commands register a `wide` codec showing additional detail columns
- [ ] Error messages include actionable suggestions with exact CLI commands
- [ ] No `os.Exit()` calls in command code — return errors, let `handleError` exit
- [ ] Status messages use `cmdio.Success/Warning/Error/Info`
- [ ] `--config` and `--context` inherited via `configOpts` persistent flags
- [ ] Destructive operations document `--dry-run` support
- [ ] Help text follows Section 8 standards (Short/Long/Examples)
- [ ] Push-like operations are idempotent (create-or-update)
- [ ] Data fetching is format-agnostic — do not gate fetches on `--output` value (Pattern 13)
- [ ] PromQL queries use `promql-builder` (`github.com/grafana/promql-builder/go/promql`), not string formatting (Pattern 14)
- [ ] List/get commands for CRUD resources wrap json/yaml output in K8s envelope manifests (see below)
- [ ] Table output shows `NAME` (the slug-id or user-facing identifier), not bare numeric `ID` — users need the NAME for get/update/delete commands (see Slug-ID naming below)

### Slug-ID Naming in Tables `[ADOPT]`

Providers whose APIs use numeric IDs should display the composite
`metadata.name` (e.g. `grafana-instance-health-5594`) as the `NAME` column in
table/wide output. This is the identifier users copy-paste into `get`, `update`,
and `delete` commands. Bare numeric IDs are accepted as input (for backward
compatibility) but should not be the primary display column.

Shared helpers in `internal/resources/adapter/slug.go` —
`SlugifyName`, `ExtractIDFromSlug`, `ComposeName` — implement the slug-id
convention. `SetResourceName` must extract and restore the API-level ID from
the composite name so CRUD operations work after a K8s round-trip.

Reference: Fleet (pipelines, collectors) and Synth (checks) providers.

### K8s Manifest Wrapping `[ADOPT]`

Provider list/get commands that output **CRUD resources** (resources the user can
create, update, and delete via the CLI) must wrap json/yaml output in K8s
envelope manifests (`apiVersion`/`kind`/`metadata`/`spec`) for round-trip
compatibility with push/pull. Table/wide codecs continue to receive raw domain
types for direct field access.

Commands that are **exempt** from K8s wrapping:

| Category | Examples | Rationale |
|----------|----------|-----------|
| Query/search results | `insights query`, `search entities` | Time-series and aggregation results, not storable resources |
| Operational views | `status`, `health`, `inspect` | Composite or derived data, not individual resources |
| Read-only reference data | `vendors list`, `scopes list`, `entity-types list` | Discoverable metadata, not user-managed resources |
| Singleton config | `env get`, `graph-config` | Single config objects, not collections of resources |

### Build Verification `[CURRENT]`

- [ ] `make build` succeeds
- [ ] `make tests` passes with no regressions
- [ ] `make lint` passes
- [ ] `gcx providers` lists the new provider
- [ ] `gcx config view` redacts secrets correctly

---

## 8. Help Text Standards

### 8.1 Command Descriptions `[ADOPT]`

| Field | Convention | Example |
|-------|-----------|---------|
| `Use` | `verb [RESOURCE_SELECTOR]...` | `list`, `get [SELECTOR]...` |
| `Short` | One sentence, period-terminated, no leading article | `List SLO definitions.` |
| `Long` | Expands on Short with usage context. 2-4 sentences. | `List all SLO definitions...` |

**Short** should start with a verb (imperative mood):

```go
// Good
Short: "List SLO definitions."
Short: "Push local resources to Grafana."

// Bad
Short: "A command that lists SLO definitions"
Short: "Lists SLOs"  // missing period
```

### 8.2 Examples Format `[CURRENT]`

Examples are prefixed with a comment explaining intent. Show 3-5 examples per
command, progressing from simple to complex:

```go
Example: `  # List all SLOs
  gcx slo list

  # List SLOs with JSON output
  gcx slo list -o json

  # List SLOs from a specific context
  gcx slo list --context=prod`,
```

### 8.3 Help Topics `[PLANNED]`

Dedicated help pages for cross-cutting concerns:

| Topic | Content |
|-------|---------|
| `gcx help environment` | All env vars (Section 10) |
| `gcx help formatting` | Output format guide, jq patterns |
| `gcx help exit-codes` | Exit code reference (Section 2) |

Implemented as Cobra help topic commands. Tracked by R2.1, R2.2.

---

## 9. Resource and API Naming

### 9.1 Resource Kind Names `[CURRENT]`

Follow Kubernetes conventions: PascalCase singular.

```
Dashboard, Folder, AlertRule, ContactPoint
```

Plural form is used in selectors: `dashboards/my-dash`, `folders/`.

### 9.2 File Naming `[CURRENT]`

Pull operations write files as `{Kind}.{Version}.{Group}/{Name}.{ext}`, grouped by
`GroupResourcesByKind`. Extension matches the source format (`.yaml`, `.json`).

Example: `Dashboard.v1alpha1.dashboard.grafana.app/my-dash.yaml`

The versioned directory name makes the API group and version unambiguous, which
is important when pulling multiple versions of the same resource type.

### 9.3 Config Key Naming `[CURRENT]`

| Location | Convention | Example |
|----------|-----------|---------|
| YAML | kebab-case | `org-id`, `stack-id`, `api-token` |
| Env vars | SCREAMING_SNAKE | `GRAFANA_ORG_ID`, `GRAFANA_STACK_ID` |
| Provider env | `GRAFANA_PROVIDER_{NAME}_{KEY}` | `GRAFANA_PROVIDER_SLO_TOKEN` |

Env var keys are normalized: underscores → dashes for provider key matching.

### 9.4 Flag Naming `[ADOPT]`

- **Format:** kebab-case (`--max-concurrent`, `--dry-run`, `--on-error`)
- **Boolean sense:** Positive by default. Prefer `--skip-validation` over
  `--no-validate`. The exception is `--no-color` which follows the `NO_COLOR`
  convention.
- **Short flags:** Reserve for the most common flags only (`-o`, `-p`, `-v`,
  `-e`, `-d`, `-t`). Don't assign short flags to provider-specific options.

### 9.5 URL Path Patterns `[CURRENT]`

Follow Kubernetes API conventions:

```
/apis/{group}/{version}/namespaces/{namespace}/{plural}/{name}
```

Provider commands using non-K8s APIs should document their URL patterns in
code comments.

---

## 10. Environment Variable Reference

> Canonical reference for all env vars. Other docs should link here.

### Core Variables `[CURRENT]`

| Variable | Scope | Effect |
|----------|-------|--------|
| `GRAFANA_SERVER` | context | Grafana server URL |
| `GRAFANA_TOKEN` | context | API token (precedence over user/pass) |
| `GRAFANA_USER` | context | Basic auth username |
| `GRAFANA_PASSWORD` | context | Basic auth password |
| `GRAFANA_ORG_ID` | context | On-prem org ID (namespace) |
| `GRAFANA_STACK_ID` | context | Cloud stack ID (namespace) |
| `GCX_CONFIG` | global | Config file path override |
| `NO_COLOR` | global | Disable color output ([no-color.org](https://no-color.org/)) |

### Provider Variables `[CURRENT]`

Pattern: `GRAFANA_PROVIDER_{NAME}_{KEY}=value`

| Variable | Provider | Key |
|----------|----------|-----|
| `GRAFANA_PROVIDER_SLO_TOKEN` | slo | token |
| `GRAFANA_PROVIDER_SLO_ORG_ID` | slo | org-id |
| `GRAFANA_PROVIDER_SM_TOKEN` | sm | token |
| `GRAFANA_PROVIDER_SM_URL` | sm | url |

Provider names and keys are case-normalized. Env vars override YAML config.

See [config-system.md](../architecture/config-system.md) for the loading chain and
[provider-guide.md](provider-guide.md) for the `ConfigKeys()` pattern.

### Implemented Variables `[CURRENT]`

| Variable | Effect | Documentation |
|----------|--------|---------------|
| `GCX_AUTO_APPROVE` | Auto-enable `--force` on delete operations | See `docs/reference/environment-variables/` |

Accepts: `1`, `true`, `0`, `false` (parsed by `caarlos0/env/v11`)

**Implementation:** `internal/config/cli_options.go` - `CLIOptions` struct loaded via `LoadCLIOptions()`

### Agent Mode Variables `[CURRENT]`

| Variable | Source | Effect |
|----------|--------|--------|
| `GCX_AGENT_MODE` | Explicit opt-in/out | `1`/`true`/`yes` enables agent mode; `0`/`false`/`no` disables (overrides all others) |
| `CLAUDE_CODE` | Claude Code | Truthy value activates agent mode |
| `CURSOR_AGENT` | Cursor | Truthy value activates agent mode |
| `GITHUB_COPILOT` | GitHub Copilot | Truthy value activates agent mode |
| `AMAZON_Q` | Amazon Q | Truthy value activates agent mode |

Detection runs at `init()` time in `internal/agent/agent.go`. See Section 6.1 for
full detection priority and the `--agent` flag.

---

## 11. Codec Requirements by Command Type `[ADOPT]`

| Command type | `text` (table) | `wide` | `json` | `yaml` | Domain-specific |
|---|---|---|---|---|---|
| CRUD data (list, get) | Required, default | Required | Built-in | Built-in | — |
| CRUD mutation (push, pull, delete) | Required, default (summary) | Required (summary) | Built-in (summary) | Built-in (summary) | — |
| Extension (status, timeline...) | Required, default | Optional | Built-in | Built-in | Optional (e.g. graph) |

All data-display and mutation commands must register a `text` table codec
and call `DefaultFormat("text")`. The `text` codec is the human default;
`json` becomes the default only in agent mode.

Codec registration happens in `setup(flags)`, not in `RunE`.

---

## 12. Mutation Command Output `[ADOPT]`

### 12.1 Summary Table

CRUD mutation commands (push, pull, delete) output a structured summary
through the codec system. The summary replaces ad-hoc `cmdio.Success/Warning`
status messages as the primary output.

**STDOUT** — summary table grouped by resource kind:

| RESOURCE KIND | TOTAL | SUCCEEDED | SKIPPED | FAILED |
|---|---|---|---|---|
| Dashboard | 2452 | 2440 | 2 | 10 |
| Folder | 48 | 48 | 0 | 0 |

**STDERR** — failures enumerated individually with error detail:

| RESOURCE | ERROR |
|---|---|
| dashboards/revenue-overview | 409 conflict: resource modified server-side |
| dashboards/checkout-funnel | 413 payload too large |

**Rules:**
- Successes are counted, never enumerated individually.
- Failures are always enumerated individually — they require action.
- Skipped resources are enumerated if count < 20, otherwise grouped.
- `cmdio.Success/Warning/Error` remain for progress feedback *during*
  execution. The summary table is the *final* output.

### 12.2 JSON Summary Shape

```json
{
  "summary": [
    {"kind": "Dashboard", "total": 2452, "succeeded": 2440, "skipped": 2, "failed": 10}
  ],
  "failures": [
    {"name": "dashboards/revenue-overview", "error": "409 conflict: resource modified server-side"}
  ],
  "skipped": [
    {"name": "dashboards/archived-q3", "reason": "no changes detected"}
  ]
}
```

Verbose opt-in (`-v` or `-o wide`) adds a `"succeeded"` array for audit.

---

## 13. Pull Format Consistency `[ADOPT]`

`pull` accepts a `--format` flag (values: `yaml`, `json`; default: `yaml`)
that enforces consistent file format on disk. All pulled files use the
specified format regardless of the server's response format.

Files are written as `plural.version.group/name.{ext}` where `{ext}`
matches the chosen format (`.yaml` or `.json`).

---

## 14. Provider / Resources Output Consistency `[ADOPT]`

Provider CRUD commands must use their registered `ResourceAdapter` (via
TypedCRUD) for data access, not raw REST clients. This ensures:

- JSON/YAML output is identical to the `resources` pipeline by construction.
- Table/wide codecs may access domain types `T` for richer columns (e.g.
  SLI%, burn rate, budget remaining).
- The `resources` pipeline uses generic resource columns (name, namespace,
  age) for its table codec.

Provider commands that bypass the adapter for CRUD operations are
non-compliant. Extension commands (status, timeline, etc.) may use raw
clients since they have no `resources` pipeline equivalent.

---

## 15. TypedCRUD Pattern `[ADOPT → EVOLVE]`

TypedCRUD is the current required pattern for new providers implementing
ResourceAdapter. It bridges typed domain objects to Kubernetes-style
unstructured envelopes.

**Current requirement:** New providers must use TypedCRUD for adapter
registration.

**Trajectory:** Domain types should be designed with eventual K8s metadata
interface compliance in mind (metadata.name, metadata.namespace,
apiVersion/kind). The long-term goal is typed resources that satisfy K8s
interfaces directly, eliminating the TypedCRUD bridge.

Do not introduce new serialization bridges, dispatch patterns, or
type-erasure mechanisms. If TypedCRUD does not fit your use case, raise
the issue for architectural discussion.

---

## 16. Provider ConfigLoader `[ADOPT]`

All provider commands must use `providers.ConfigLoader` for flag binding
(`--config`, `--context`) and config resolution (YAML + env var precedence).

### ConfigLoader API

| Method | Purpose | Used by |
|--------|---------|---------|
| `LoadGrafanaConfig(ctx)` | REST config for Grafana API calls | alert, fleet, incidents, kg, oncall, slo, synth |
| `LoadCloudConfig(ctx)` | Cloud token + GCOM stack info | k6, fleet |
| `LoadProviderConfig(ctx, name)` | Provider-specific `map[string]string` + namespace | synth, oncall, k6 |
| `SaveProviderConfig(ctx, name, key, val)` | Write-back a single provider config key | synth (datasource UID) |
| `LoadFullConfig(ctx)` | Full `*config.Config` (for cross-cutting lookups) | synth (datasource discovery) |

### Provider-specific config pattern

Providers that need custom keys (URLs, tokens, domain overrides) use
`LoadProviderConfig` instead of ad-hoc `os.Getenv` or `ProviderConfig` map
access. This ensures `GRAFANA_PROVIDER_<NAME>_<KEY>` env vars, config file
values, and `--context` switching all work uniformly:

```go
// In provider's config loader or adapter factory:
providerCfg, namespace, err := l.LoadProviderConfig(ctx, "synth")
if err != nil {
    return err
}
smURL := providerCfg["sm-url"]  // resolved from env or config file
```

Provider-specific defaults and fallbacks (e.g., `DefaultAPIDomain` for k6,
plugin discovery for oncall) remain in the provider package — `ConfigLoader`
is generic.

### Do not

- Import `cmd/gcx/config` from provider code (import cycle)
- Roll custom flag binding for `--config`/`--context`
- Construct HTTP clients or load credentials outside ConfigLoader
- Hardcode env var names — ConfigLoader handles `GRAFANA_PROVIDER_*` resolution
- Use `os.Getenv` for provider-specific env vars — use `LoadProviderConfig`
- Swallow errors from `LoadProviderConfig` — propagate them; only fall through
  to alternative resolution when the key is absent, not when config loading fails

---

## Appendix: Recommendation Traceability

Maps sections to the cli-analysis recommendations (R1.1–R3.5):

| R# | Description | Section | Status |
|----|-------------|---------|--------|
| R1.1 | Exit code taxonomy | 2 | `[CURRENT]` |
| R1.2 | Auto-approve | 3.2, 3.3 | `[IMPLEMENTED]` (3.2) / `[PLANNED]` (3.3) |
| R1.3 | Agent mode | 6 | `[CURRENT]` (detection + format/color + pipe behavior + in-band errors) / `[PLANNED]` (auto-approve) |
| R2.1 | Help formatting page | 8.3 | `[PLANNED]` |
| R2.2 | Help environment page | 10, 8.3 | `[CURRENT]` / `[PLANNED]` |
| R2.3 | Automation guide | — | Out of scope (separate doc) |
| R3.1 | JSON field discovery | 1.5 | `[CURRENT]` |
| R3.2 | API escape hatch | — | Out of scope (feature) |
| R3.3 | Pipe detection | 5 | `[CURRENT]` |
| R3.4 | Push idempotency | 3.5 | `[CURRENT]` |
| R3.5 | In-band error reporting | 4.4 | `[CURRENT]` |

---

*Source: cross-referenced against codebase as of 2026-03-04.*
