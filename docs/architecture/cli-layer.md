# CLI Layer and Command Patterns

## Command Composition

The root Cobra command assembles built-in groups and registered provider commands. `cmd/gcx` owns executable wiring, while provider factories contribute their product surfaces from `internal/providers`. Resolve the current command and flag surface from built-in help or the [released CLI reference](../sources/cli-reference.md), rather than maintaining a second command tree here.

CLI wiring validates input, loads configuration at execution time, calls domain operations, and renders their results. Business behavior belongs in the relevant domain package. Command placement and naming follow [command naming](../design/command-naming.md).

## Agent Identity

`internal/agent` resolves native signals and supported explicit names.
Command defaults and usage telemetry use the same detector.
`GCX_AGENT_NAME` supports harnesses without a native marker.
Only fixed labels reach telemetry. Test helpers clear the detector's full
input list through `agent.EnvironmentVariables()`.
See [agent mode](../design/agent-mode.md#61-detection) for precedence.

## Portable Skill Lifecycle

`claude-plugin/assets.go` embeds `skills/` and `skills-catalog.yaml` separately.
`internal/skills` decodes active/deprecated/retired metadata and reconciles it with
the selected local `.agents` root. `TestBundledCatalog` checks content consistency
and replacement existence at build time, not on the uninstall recovery path.
It also requires the catalog to retain every name in the committed, append-only
`internal/skills/testdata/shipped_skills.txt` snapshot, independently of current
bundle contents. New bundled names must be appended to that snapshot. Install, update, list,
and uninstall share this read-only reconciliation; get reads bundled content only.
CLI wiring and codecs remain in `cmd/gcx/skills`; the background update notifier
uses the same update preview, including retirement notices.

Retired catalog entries remain after content removal so old installations stay
manageable across skipped releases. Uncataloged local directories are unmanaged.
Catalog membership does not prove ownership; no installation manifest or file
pruning is implemented. See [skill lifecycle](https://github.com/grafana/gcx/blob/main/claude-plugin/README.md#skill-lifecycle)
for maintenance rules and command behavior.

## Provider Command Groups

Providers contribute product-specific command groups to gcx. They can use native Grafana resources or product REST APIs. Resources backed by a product API use adapters to participate in the generic resource pipeline.

### When to use a provider vs `resources`

```
Do you need only standard CRUD on an externally accessible, discoverable
/apis resource?
├── YES → `gcx resources` covers it; no provider needed for CRUD
└── NO  → Create a provider (wraps the product's REST API)
```

Being on `/apis` settles CRUD, not the command surface: `gcx dashboards` and
`gcx alert` are dedicated command trees over `/apis`-backed products, because
their real operations are not CRUD verbs. Product-specific operations need their
own placement analysis regardless of tier. Provider commands over a native
`/apis` resource go through the shared native binding (`internal/providers/native`,
see `CONSTITUTION.md` § Architecture Invariants); dashboards still hand-rolls this
access until its pending migration lands.

See [provider placement](../../.agents/skills/contribute/references/provider-placement.md) for the full
decision tree.

### Provider command structure

Providers contribute product-specific command groups, with resource-type groups where appropriate. Expose supported operations according to the command contract. Human CRUD commands must use the same typed resource conversion and adapter behavior as the generic resource pipeline. Operational views and query results can use their own result types.

See the [provider guide](../reference/provider-guide.md) for the implementation recipe and [resource routing](resource-model.md#5b-provider-backed-resources-resourceadapter-typedcrud-and-router) for the adapter contract.

### Config loading pattern

Provider commands cannot import `cmd/gcx/config` (import cycle). Instead,
they use a shared, exported `providers.ConfigLoader` that binds the `--config`
flag. The `--context` flag is owned by the root command and threaded into
provider commands via `context.Context`. See `internal/providers/configloader.go`
for the reference implementation.

```go
// Shared across all providers — defined in internal/providers/configloader.go
loader := &providers.ConfigLoader{}
loader.BindFlags(sloCmd.PersistentFlags())  // --config flag (root owns --context)

func (l *ConfigLoader) LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error) {
    // Applies env vars (GRAFANA_TOKEN, GRAFANA_PROVIDER_*), context flag,
    // and validates. See internal/providers/configloader.go for the full implementation.
}
```

### Adding a new provider

Start with the [`contribute`](../../.agents/skills/contribute/SKILL.md) skill and
its provider implementation reference; [provider-guide.md](../reference/provider-guide.md)
contains the canonical implementation recipe.

---

## The Options Pattern

Every command in the `resources` package follows the same struct pattern. `push.go` is the canonical example:

```go
// 1. Declare an opts struct holding all command-specific state.
type pushOpts struct {
    Paths         []string
    MaxConcurrent int
    OnError       OnErrorMode   // shared type from onerror.go
    DryRun        bool
    // ...
}

// 2. setup binds CLI flags to struct fields.
//    Called once at command construction time (not at execution time).
func (opts *pushOpts) setup(flags *pflag.FlagSet) {
    flags.StringSliceVarP(&opts.Paths, "path", "p", []string{defaultResourcesPath}, "...")
    flags.IntVar(&opts.MaxConcurrent, "max-concurrent", 10, "...")
    bindOnErrorFlag(flags, &opts.OnError)  // shared flag helper
    flags.BoolVar(&opts.DryRun, "dry-run", opts.DryRun, "...")
}

// 3. Validate checks semantic constraints on the parsed flag values.
//    Called at the START of RunE, before any I/O.
func (opts *pushOpts) Validate() error {
    if len(opts.Paths) == 0 {
        return errors.New("at least one path is required")
    }
    if opts.MaxConcurrent < 1 {
        return errors.New("max-concurrent must be greater than zero")
    }
    return opts.OnError.Validate()
}

// 4. Constructor function wires everything together.
func pushCmd(configOpts *cmdconfig.Options) *cobra.Command {
    opts := &pushOpts{}

    cmd := &cobra.Command{
        Use:   "push [RESOURCE_SELECTOR]...",
        RunE: func(cmd *cobra.Command, args []string) error {
            if err := opts.Validate(); err != nil { return err }
            // ... execution body
        },
    }

    opts.setup(cmd.Flags())  // bind flags AFTER command is created
    return cmd
}
```

The parent group (`config.Command()` or `resources.Command()`) owns `configOpts` and passes it down:

```go
// resources/command.go
func Command() *cobra.Command {
    configOpts := &cmdconfig.Options{}      // one shared instance
    cmd := &cobra.Command{Use: "resources"}
    configOpts.BindFlags(cmd.PersistentFlags())  // --config, --context persistent

    cmd.AddCommand(pushCmd(configOpts))     // injected into every subcommand
    cmd.AddCommand(pullCmd(configOpts))
    // ...
    return cmd
}
```

**Rule:** `config.Options` is always a persistent flag set on the group, never on individual subcommands.

---

## Command Lifecycle

```
User invokes: gcx resources push dashboards/foo -p ./resources

cobra.Execute()
    │
    ├─ PersistentPreRun [root/command.go]
    │       Configures slog verbosity, klog logger.
    │       Attaches logger to cmd.Context() via logging.Context().
    │
    └─ RunE [push.go]
            │
            ├─ 1. opts.Validate()
            │       Checks flag constraints (paths non-empty, concurrency > 0, etc.)
            │       Returns error immediately if invalid — no I/O performed yet.
            │
            ├─ 2. configOpts.LoadGrafanaConfig(ctx)
            │       Loads config file (--config flag or XDG standard location).
            │       Applies env var overrides (GRAFANA_SERVER, GRAFANA_TOKEN, ...).
            │       Applies --context override if set.
            │       Validates context exists and credentials present.
            │       Returns NamespacedRESTConfig (server URL + namespace + auth).
            │
            ├─ 3. resources.ParseSelectors(args)
            │       Parses "dashboards/foo" into PartialGVK + resource UIDs.
            │
            ├─ 4. discovery.NewDefaultRegistry(ctx, cfg)
            │       Calls Grafana's ServerGroupsAndResources endpoint.
            │       Builds GVK index. Filters out read-only/internal groups.
            │
            ├─ 5. reg.MakeFilters(...)
            │       Resolves partial selectors to fully-qualified Descriptors.
            │
            ├─ 6. Command-specific I/O (push: read files, call Grafana API)
            │       local.FSReader.Read(...)
            │       remote.NewDefaultPusher(...).Push(...)
            │
            └─ 7. Output summary
                    cmdio.Success/Warning/Error(...) — colored status line
                    Return non-nil error to trigger handleError in main.go
```

**Error propagation:** `RunE` returns an error. `main.go:handleError` calls `fail.ErrorToDetailedError` which converts the raw error into a `DetailedError` with a structured, colored rendering. The original error is never printed directly to stderr.

---

## Shared Helpers

### `fetch.go` — Remote Fetch Abstraction

`get`, `edit`, and `delete` all need to fetch resources from Grafana before acting on them. `fetchResources` centralizes this:

```go
// fetch.go
type fetchRequest struct {
    Config             config.NamespacedRESTConfig
    StopOnError        bool
    ExcludeManaged     bool
    ExpectSingleTarget bool   // enforces single-resource selectors (used by edit)
    Processors         []remote.Processor
}

func fetchResources(ctx context.Context, opts fetchRequest, args []string) (*fetchResponse, error)
```

Usage in `get.go`:
```go
res, err := fetchResources(ctx, fetchRequest{
    Config:      cfg,
    StopOnError: opts.OnError.StopOnError(),
}, args)
```

Usage in `edit.go` (single-target enforcement):
```go
res, err := fetchResources(ctx, fetchRequest{
    Config:             cfg,
    StopOnError:        true,
    ExpectSingleTarget: true,   // errors if selector isn't KIND/name
}, args)
```

### `onerror.go` — Error Mode

All multi-resource commands expose `--on-error` via a shared helper:

```go
type OnErrorMode string  // "ignore" | "fail" | "abort"

func bindOnErrorFlag(flags *pflag.FlagSet, target *OnErrorMode)
func (m OnErrorMode) StopOnError() bool   // abort → true
func (m OnErrorMode) FailOnErrors() bool  // fail|abort → true
func (m OnErrorMode) Validate() error
```

Commands add this to their opts struct and delegate to it:
```go
// In opts struct:
OnError OnErrorMode

// In setup():
bindOnErrorFlag(flags, &opts.OnError)

// In Validate():
return opts.OnError.Validate()

// In RunE():
StopOnError: opts.OnError.StopOnError()
// ...
if opts.OnError.FailOnErrors() && summary.FailedCount() > 0 {
    return fmt.Errorf(...)
}
```

### `editor.go` — Interactive Editing

`editorFromEnv()` reads `$EDITOR` (fallback: `vi`/`notepad`) and `$SHELL`. The `editor` type provides:

```go
// Open a specific file path in the editor
func (e editor) Open(ctx context.Context, file string) error

// Write buffer to a temp file, open it, return modified contents
func (e editor) OpenInTempFile(ctx context.Context, buffer io.Reader, format string) (cleanup func(), contents []byte, err error)
```

`edit.go` uses `OpenInTempFile`: it fetches a resource, serializes it, opens the editor, reads back the modified bytes, then pushes changes if the content differs from the original.

---

## Output Formatting (`internal/output/`)

> See also [output.md](../design/output.md) for output contract and [exit-codes.md](../design/exit-codes.md) for
> exit code taxonomy, and default format conventions.

### `io.Options` — Format Selection

Embedded in command opts structs to add `--output / -o` and `--json` flag support:

```go
type Options struct {
    OutputFormat  string
    JSONFields    []string   // set when --json field1,field2 is used
    JSONDiscovery bool       // set when --json ? is used
    IsPiped       bool       // true when stdout is not a TTY (from terminal.IsPiped())
    NoTruncate    bool       // true when --no-truncate or stdout is piped
    customCodecs  map[string]format.Codec
    defaultFormat string
}

// In command opts setup():
opts.IO.DefaultFormat("text")                          // set default
opts.IO.RegisterCustomCodec("text", &tableCodec{})     // add command-specific codec
opts.IO.RegisterCustomCodec("wide", &tableCodec{wide: true})
opts.IO.BindFlags(flags)                               // registers --output/-o and --json flags

// In RunE:
codec, err := opts.IO.Codec()   // resolves the selected format to a format.Codec
codec.Encode(cmd.OutOrStdout(), data)
```

**`IsPiped` and `NoTruncate`** are populated during `BindFlags` from the
`internal/terminal` package-level state, which is set by root `PersistentPreRun`
before any command runs. Table codecs should read `opts.IO.NoTruncate` to
decide whether to truncate long column values. See [pipe-awareness.md](../design/pipe-awareness.md).

**`--json` flag** is registered by `BindFlags` on the command's `FlagSet`.
`Validate()` calls `applyJSONFlag()` which:
1. Enforces mutual exclusion with `-o/--output`
2. Sets `JSONDiscovery=true` when the value is `?`
3. Parses comma-separated field names into `JSONFields`
4. Forces `OutputFormat="json"` when field names are given

When `JSONFields` is set, callers should use `NewFieldSelectCodec(opts.IO.JSONFields)`
instead of `opts.IO.Codec()`. When `JSONDiscovery` is set, callers should
print available fields via `DiscoverFields()` and exit early (exit 0).

**List envelopes.** Field selection and discovery descend into provider list
results rather than operating on the wrapper object: a single-key envelope
(`{"datasources": [...]}`) is detected structurally, and a multi-key envelope
carrying list-level metadata (e.g. `{"investigations": [...], "total": 42}`)
opts in by implementing `output.ListEnvelope` (`ListItemsKey() string`,
satisfied structurally — no import of `internal/output` needed). The declared
key and metadata siblings are preserved in field-selected output, discovery
samples item fields (reflecting on the declared slice field when the list is
empty), and the agents codec's spill summary previews and counts the
envelope's items. The opt-in is deliberate: a structural multi-key heuristic
would misclassify detail objects that happen to contain one nested array.

Use `Options.Encode` for structured output: `Codec().Encode` bypasses jq,
field selection, and discovery. jq runs before formatting or spilling; see
[the output contract](../design/output.md#16-jq-transformation).

Built-in codecs: `json`, `yaml`, and `agents` (always available). Commands register additional ones (e.g. `text`, `wide`, `graph`) by calling `RegisterCustomCodec` before `BindFlags`.

The `graph` codec is a special-purpose output format available on per-kind `query` subcommands (`metrics query`, `logs query`, `profiles metrics`, etc.) and `synth checks status`. It renders Prometheus or Loki query results (or check status metrics) as a terminal line chart using `ntcharts` and `lipgloss` (via `internal/graph`). Terminal width is detected at render time via `golang.org/x/term`.

The `wide` codec is available on `slo definitions list`, `slo reports list`, and `synth checks status`. It shows additional detail columns compared to the default `text` table codec.

### `FieldSelectCodec` — JSON Field Filtering

`internal/output/field_select.go` provides `FieldSelectCodec`, which wraps
the built-in JSON codec and emits only the requested fields from each object:

```go
// Construct with the parsed field list from io.Options.JSONFields:
codec := io.NewFieldSelectCodec(opts.IO.JSONFields)
if err := codec.Encode(cmd.OutOrStdout(), resources); err != nil {
    return err
}
```

**Supported input types:** `unstructured.Unstructured`, `*unstructured.Unstructured`,
`unstructured.UnstructuredList`, `*unstructured.UnstructuredList`, `map[string]any`,
and arbitrary types (marshaled to JSON, then fields extracted).

**Output shapes:**

| Input | Output |
|-------|--------|
| Single object | `{"field": value, ...}` |
| List/collection | `{"items": [{"field": value}, ...]}` |

**Dot-path resolution:** `metadata.name` walks `obj["metadata"]["name"]`.
Missing paths produce `null` — never omitted, never an error (FR-008).

**Field discovery** is handled by `DiscoverFields(obj map[string]any) []string`:
returns top-level keys plus `spec.*` sub-keys, sorted alphabetically. Call this
on a sample object fetched from the API.

### Custom Table Codecs

Ordinary provider tables declare columns with `output.Table[T]` (see
[RFC 025](../rfcs/025-shared-table-declaration.md)).
For wrapper or singleton payloads, `output.TableAdapter[P, T]` extracts rows
inside the human codec while JSON/YAML retain the complete payload:

```go
table := output.TableAdapter[Result, Item]{
    Table: itemTable(),
    Rows: func(result Result) ([]Item, error) { return result.Items, nil },
}
opts.IO.RegisterCustomCodec("table", table.Codec("table"))
opts.IO.RegisterCustomCodec("wide", table.Codec("wide"))
```

Register only the formats the command supports, before `BindFlags`, and pass the
complete result to `opts.IO.Encode` for every format. Specialised reports retain
custom `format.Codec` implementations. Resource commands also use Kubernetes
table printers and `text/tabwriter` for their specialised output.

### Status Messages (`messages.go`)

Four colored message functions output to a given `io.Writer`:

```go
cmdio.Success(cmd.ErrOrStderr(), "%d resources pushed, %d errors", ok, fail)
cmdio.Warning(cmd.ErrOrStderr(), "...")
cmdio.Error(cmd.ErrOrStderr(), "...")
cmdio.Info(cmd.ErrOrStderr(), "...")
```

Pass the **stderr** writer: stdout carries the result, stderr the diagnostics
(see [output.md § 1.4](../design/output.md) and CONSTITUTION.md § Output).

They prefix with colored Unicode symbols (✔ ⚠ ✘ 🛈). `--no-color` disables all color globally via `color.NoColor = true` in root's `PersistentPreRun`.

---

## Error Handling (`cmd/gcx/fail/`)

> See also [errors.md](../design/errors.md) for error design guidelines,
> writing good suggestions, and exit code assignments.

### `DetailedError` — Structured Error Type

```go
type DetailedError struct {
    Summary     string    // required: one-liner title
    Details     string    // optional: multi-line explanation
    Parent      error     // optional: wrapped underlying error
    Suggestions []string  // optional: actionable hints
    DocsLink    string    // optional: URL
    ExitCode    *int      // optional: override default exit code 1
}
```

Renders as:
```
Error: Resource not found
│
├─ Details:
│
│ dashboards.v0alpha1.dashboard.grafana.app "nonexistent" not found
│
├─ Suggestions:
│
│ • Make sure that your are passing in valid resource selectors
│
└─
```

Commands can return a `DetailedError` directly from `RunE`. Business logic layers can also return them (e.g. `fetch.go` returns one when `ExpectSingleTarget` is violated).

### Command Usage and Server Capabilities

`internal/gcxerrors.UsageError` and `NewCommandUsageError` provide the same
expected usage line and help suggestion to command implementations in both
`cmd/` and `internal/`. The `cmd/gcx/fail` exports remain compatibility wrappers;
the converter renders these errors as `Invalid command usage` with exit 2.

Malformed resource selectors also exit 2, with suggestions that apply to any
resource command. Discovery returns `resources.UnsupportedResourceError` when
a well-formed resource type or API version is absent from the server. This is
`Endpoint not available` with exit 1, including dashboard commands using their
default API version. It does not imply incorrect command syntax.

### `ErrorToDetailedError` — Error Conversion Pipeline

`main.go:handleError` calls this on any error before printing. It runs a chain of type-specific converters:

```
ErrorToDetailedError(err)
    │
    ├─ errors.As(err, &DetailedError{}) → return as-is if already detailed
    ├─ ... domain and transport converters (specific errors first)
    ├─ convertHTTPStatusErrors → concrete gcxerrors.HTTPStatusError
    └─ fallbackDetailedError  → Unexpected error
```

`convertHTTPStatusErrors` is the final typed converter, immediately before the
fallback. This preserves specialized handling for errors that carry domain
context while still classifying otherwise-unhandled HTTP failures. It matches
the concrete `gcxerrors.HTTPStatusError` type through `errors.As`; the shared
`HTTPStatusCode()` interface is used by telemetry and is not the converter's
match condition.

The HTTP status determines the broad classification: 401 is `Authentication
failed`, 403 is `Authorization failed`, 404 is `Resource not found`, 409 is
`Resource conflict`, and other statuses are `API error`. Authentication and
authorization both use exit 3. An HTML response does not change this
status-based summary or exit code; converters replace HTML response details
with a generic explanation so an SSO page is not rendered as an API message.
The raw `gcx api` command is exempt from that rendering rule and keeps its full
response body as passthrough output.

Every converter summary uses an exported constant from `internal/gcxerrors`.
The converter tests enforce that rule with an AST check (including helper
returns), check that the constants match the summary table in
`docs/design/errors.md`, and verify produced summaries at runtime. Whole-struct
copies of an already-converted `DetailedError` are allowed. The fallback uses
`Unexpected error` and puts the complete original message in `Details`; it
leaves `Parent` unset so the message is not repeated.

**Adding new error conversions:** add a `convertXxxErrors` function following
the `func(error) (*DetailedError, bool)` signature and append it to the
`errorConverters` slice in `ErrorToDetailedError`. Keep the HTTP status
converter last among typed converters, and use a summary constant from
`internal/gcxerrors`.

---

## How Config Flows Through Commands

`config.Options` is a reusable struct that bundles the `--config` and `--context` flags with three loading methods:

```
config.Options
├── BindFlags(flags)         — registers --config, --context flags
├── loadConfigTolerant(ctx)  — loads without full validation (config subcommands)
├── LoadConfig(ctx)          — loads + validates context + credentials
└── LoadGrafanaConfig(ctx)   — LoadConfig + constructs NamespacedRESTConfig
```

`resources.Command()` creates one `configOpts` instance, binds it to persistent flags on the group, then passes it by pointer into every subcommand constructor. Subcommands call `configOpts.LoadGrafanaConfig(ctx)` at execution time (not construction time), ensuring the flag values are already parsed.

---

## Convention: Adding a New `resources` Subcommand

1. Follow the options and lifecycle patterns above in the resource command package.
2. Reuse configuration, fetch, output, and error helpers; register custom codecs before binding output flags.
3. Register the constructor with the resource command group.
4. Verify the command contract through the [contributor workflow](../../.agents/skills/contribute/SKILL.md).

Use an existing neighboring command as the implementation example so imports, helper names, and wiring stay current.

## Key Invariants

- `opts.Validate()` is the FIRST call in `RunE`
- `configOpts.LoadGrafanaConfig` is called in `RunE`, not at construction
- `--config` and `--context` are persistent on the group, not per-subcommand
- All errors bubble up through `RunE` return value; never `os.Exit` in commands
- Status messages go to `cmd.ErrOrStderr()`, never `os.Stdout`/`os.Stderr` directly
- Custom table codecs implement `format.Codec` and are registered before `BindFlags`
- Data fetching is format-agnostic — fetch all fields, let codecs filter display
- `OnErrorMode` is always validated in `opts.Validate()`, not inline
- `terminal.Detect()` is called once in `PersistentPreRun`; use `terminal.IsPiped()` / `terminal.NoTruncate()` everywhere else
- `--json` is mutually exclusive with `-o/--output`; enforced in `io.Options.Validate()`
