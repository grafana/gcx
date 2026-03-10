# CLI Layer and Command Patterns

## Command Tree

```
grafanactl (root)
├── --no-color               [persistent flag]
├── --no-truncate            [persistent flag: disable table column truncation]
├── --agent                  [persistent flag: enable agent mode]
├── --verbose / -v           [persistent flag, count]
│
├── api                      [cmd/grafanactl/api/command.go]
│   ├── --config             [persistent: inherited from config.Options]
│   ├── --context            [persistent: inherited from config.Options]
│   ├── --method / -X        HTTP method (default: GET, or POST if -d is set)
│   ├── --data / -d          Request body (@file, @-, or literal). Implies POST.
│   ├── --header / -H        Custom headers (repeatable)
│   └── --output / -o        json|yaml  [default: json]
│
├── config                   [cmd/grafanactl/config/command.go]
│   ├── --config             [persistent: path to config file]
│   ├── --context            [persistent: context override]
│   ├── check
│   ├── current-context
│   ├── list-contexts
│   ├── set      PROPERTY_NAME PROPERTY_VALUE
│   ├── unset    PROPERTY_NAME
│   ├── use-context CONTEXT_NAME
│   └── view
│       └── --output / -o   [yaml|json, default: yaml]
│
├── resources                [cmd/grafanactl/resources/command.go]
│   ├── --config             [persistent: inherited from config.Options]
│   ├── --context            [persistent: inherited from config.Options]
│   ├── delete [SELECTOR]...
│   ├── edit   SELECTOR
│   ├── get    [SELECTOR]...
│   ├── list
│   ├── pull   [SELECTOR]...
│   ├── push   [SELECTOR]...
│   ├── serve  [DIR]...
│   └── validate [SELECTOR]...
│
├── datasources              [cmd/grafanactl/datasources/command.go]
│   ├── --config             [persistent: inherited from config.Options]
│   ├── --context            [persistent: inherited from config.Options]
│   ├── list
│   ├── get    NAME
│   ├── prometheus           Prometheus-specific operations
│   │   ├── labels           [--datasource/-d UID] [--label/-l NAME]
│   │   ├── metadata         [--datasource/-d UID] [--metric/-m NAME]
│   │   └── targets          [--datasource/-d UID] [--state active|dropped|any]
│   └── loki                 Loki-specific operations
│       ├── labels           [--datasource/-d UID] [--label/-l NAME]
│       └── series           --match SELECTOR... [--datasource/-d UID]
│
├── query                    [cmd/grafanactl/query/command.go]
│   ├── --config             [persistent: inherited from config.Options]
│   ├── --context            [persistent: inherited from config.Options]
│   ├── --expr / -e          Query expression (PromQL or LogQL)  [required]
│   ├── --type / -t          Datasource type: prometheus|loki  [default: prometheus]
│   ├── --datasource / -d    Datasource UID (or use config default)
│   ├── --start              Start time (RFC3339, Unix, or relative e.g. now-1h)
│   ├── --end                End time (RFC3339, Unix, or relative e.g. now)
│   ├── --step               Query step (e.g. 15s, 1m)
│   └── --output / -o        table|json|yaml|graph  [default: table]
│
├── providers                [cmd/grafanactl/providers/command.go]
│   └── (list; no subcommands — prints NAME/DESCRIPTION table of registered providers)
│
├── linter                   [cmd/grafanactl/linter/command.go]
│   ├── lint                 Lint resources against configured rules
│   ├── new                  Scaffold a new linter rule
│   ├── rules                List available linter rules
│   └── test                 Run rule test suite
│
└── dev                      [cmd/grafanactl/dev/command.go]
    ├── generate [FILE_PATH]... Generate typed Go stubs for new resources
    ├── import               Import existing Grafana resources as code
    └── scaffold             Scaffold a new grafanactl-based project
```

Key: SELECTOR = `kind[/name[,name...]]` or long form `kind.group/name`

---

## Provider Command Groups

Providers contribute top-level command groups to grafanactl. Unlike `resources`
subcommands (which use the dynamic K8s client), provider commands wrap
product-specific REST APIs and translate to/from the K8s envelope format.

### When to use a provider vs `resources`

```
Does the product expose a K8s-compatible API via /apis endpoint?
├── YES → Use `grafanactl resources` (no provider needed)
└── NO  → Create a provider (wraps product's REST API)
```

See `.claude/skills/add-provider/references/decision-tree.md` for the full
decision tree.

### Provider command structure

Provider commands follow a consistent pattern: a top-level group command with
resource-type subcommands underneath. Each resource type gets standard CRUD
operations plus optional product-specific commands.

```
grafanactl {provider}           [contributed by Provider.Commands()]
├── --config                    [persistent: inherited via providers.ConfigLoader]
├── --context                   [persistent: inherited via providers.ConfigLoader]
│
├── {resource-type}             [one group per resource type]
│   ├── list                    [always: list all resources]
│   ├── get    <id>             [always: get single resource]
│   ├── push   [path...]        [always: create-or-update from local files]
│   ├── pull                    [always: export to local files]
│   ├── delete <id...>          [always: delete resources]
│   └── status [id]             [optional: operational health data]
│
└── {other-resource-type}       [if product has multiple resource types]
    └── (same CRUD pattern)
```

### Current providers

```
grafanactl slo                  [internal/providers/slo/provider.go]
├── definitions                 CRUD + status/timeline for SLO definitions
│   ├── list
│   ├── get    <uuid>
│   ├── push   [path...]
│   ├── pull
│   ├── delete <uuid...>
│   └── status [uuid]
└── reports                     CRUD + status for SLO reports
    ├── list
    ├── get    <uuid>
    ├── push   [path...]
    ├── pull
    ├── delete <uuid...>
    └── status [uuid]

grafanactl synth                [internal/providers/synth/provider.go]
├── checks                      CRUD + status/timeline for Synthetic Monitoring checks
│   ├── list
│   ├── get    <id>
│   ├── push   [path...]
│   ├── pull
│   ├── delete <id...>
│   ├── status [id]
│   └── timeline [id]
└── probes                      List Synthetic Monitoring probes
    └── list
```

### Config loading pattern

Provider commands cannot import `cmd/grafanactl/config` (import cycle). Instead,
they use a shared, exported `providers.ConfigLoader` that binds `--config` and `--context` flags
independently. See `internal/providers/configloader.go` for the reference implementation.

```go
// Shared across all providers — defined in internal/providers/configloader.go
loader := &providers.ConfigLoader{}
loader.BindFlags(sloCmd.PersistentFlags())  // --config, --context flags

func (l *ConfigLoader) LoadRESTConfig(ctx context.Context) (config.NamespacedRESTConfig, error) {
    // Applies env vars (GRAFANA_TOKEN, GRAFANA_PROVIDER_*), context flag,
    // and validates. See internal/providers/configloader.go for the full implementation.
}
```

### Adding a new provider

Follow the `/add-provider` skill or `agent-docs/provider-guide.md` for the
step-by-step implementation guide.

---

## File Layout

```
cmd/grafanactl/
├── main.go                  Entry point — wires root.Command, calls handleError
├── root/
│   └── command.go           Root cobra command: logging setup, PersistentPreRun
├── config/
│   └── command.go           config group + all config subcommands + Options type
├── resources/
│   ├── command.go           resources group (wires configOpts to all subcommands)
│   ├── get.go               resources get
│   ├── list.go              resources list
│   ├── pull.go              resources pull
│   ├── push.go              resources push
│   ├── delete.go            resources delete
│   ├── edit.go              resources edit
│   ├── validate.go          resources validate
│   ├── serve.go             resources serve
│   ├── fetch.go             SHARED: remote fetch helper used by get/edit/delete
│   ├── onerror.go           SHARED: OnErrorMode type + --on-error flag binding
│   └── editor.go            SHARED: interactive editor (EDITOR env var)
├── datasources/
│   ├── command.go           datasources group (wires configOpts to subcommands)
│   ├── list.go              datasources list
│   ├── get.go               datasources get
│   ├── prometheus.go        prometheus subgroup + labels/metadata/targets commands
│   └── loki.go              loki subgroup + labels/series commands
├── query/
│   ├── command.go           query command (--expr, --type, --datasource, --start, --end, --step)
│   └── graph.go             queryGraphCodec — terminal chart via internal/graph
├── providers/
│   └── command.go           providers command — lists registered providers
├── linter/
│   ├── command.go           linter group (lint, new, rules, test subcommands)
│   ├── lint.go              linter lint — lint resources against configured rules
│   ├── new.go               linter new — scaffold a new linter rule
│   ├── rules.go             linter rules — list available linter rules
│   └── test.go              linter test — run rule test suite
├── dev/
│   ├── command.go           dev group (generate, import, scaffold subcommands)
│   ├── generate.go          dev generate — generate typed Go stubs for new resources
│   ├── import.go            dev import — import Grafana resources as code
│   ├── scaffold.go          dev scaffold — scaffold a new project
│   └── templates/           Embedded Go templates for generate/import/scaffold
├── fail/
│   ├── detailed.go          DetailedError type — rich error formatting
│   ├── convert.go           ErrorToDetailedError — error-type dispatch table
│   └── json.go              DetailedError.WriteJSON — in-band JSON error for agent mode
└── io/
    ├── format.go            Options type — --output/-o + --json flags + codec registry
    ├── field_select.go      FieldSelectCodec — JSON field filtering + DiscoverFields
    └── messages.go          Success/Warning/Error/Info colored printers
```

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
User invokes: grafanactl resources push dashboards/foo -p ./resources

cobra.Execute()
    │
    ├─ PersistentPreRun [root/command.go:27]
    │       Configures slog verbosity, klog logger.
    │       Attaches logger to cmd.Context() via logging.Context().
    │
    └─ RunE [push.go:95]
            │
            ├─ 1. opts.Validate()
            │       Checks flag constraints (paths non-empty, concurrency > 0, etc.)
            │       Returns error immediately if invalid — no I/O performed yet.
            │
            ├─ 2. configOpts.LoadRESTConfig(ctx)
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

## Output Formatting (`cmd/grafanactl/io/`)

> See also [design-guide.md](design-guide.md) Sections 1–2 for output contract,
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
decide whether to truncate long column values. See `design-guide.md` Section 5.1.

**`--json` flag** is registered by `BindFlags` on the command's `FlagSet`.
`Validate()` calls `applyJSONFlag()` which:
1. Enforces mutual exclusion with `-o/--output`
2. Sets `JSONDiscovery=true` when the value is `?`
3. Parses comma-separated field names into `JSONFields`
4. Forces `OutputFormat="json"` when field names are given

When `JSONFields` is set, callers should use `NewFieldSelectCodec(opts.IO.JSONFields)`
instead of `opts.IO.Codec()`. When `JSONDiscovery` is set, callers should
print available fields via `DiscoverFields()` and exit early (exit 0).

Built-in codecs: `json` and `yaml` (always available). Commands register additional ones (e.g. `text`, `wide`, `graph`) by calling `RegisterCustomCodec` before `BindFlags`.

The `graph` codec is a special-purpose output format only available on the `query` command. It renders Prometheus or Loki query results as a terminal line chart using `ntcharts` and `lipgloss` (via `internal/graph`). Terminal width is detected at render time via `golang.org/x/term`.

### `FieldSelectCodec` — JSON Field Filtering

`cmd/grafanactl/io/field_select.go` provides `FieldSelectCodec`, which wraps
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

Commands define their own table-rendering codec by implementing `format.Codec`:

```go
type tableCodec struct { wide bool }

func (c *tableCodec) Format() format.Format { return "text" }
func (c *tableCodec) Encode(output io.Writer, input any) error { /* render table */ }
func (c *tableCodec) Decode(io.Reader, any) error { return errors.New("not supported") }
```

`get.go` uses `k8s.io/cli-runtime/pkg/printers.NewTablePrinter` to produce kubectl-style output. `list.go` and `validate.go` use `text/tabwriter` directly.

### Status Messages (`messages.go`)

Four colored message functions output to a given `io.Writer`:

```go
cmdio.Success(cmd.OutOrStdout(), "%d resources pushed, %d errors", ok, fail)
cmdio.Warning(cmd.OutOrStdout(), "...")
cmdio.Error(cmd.OutOrStdout(), "...")
cmdio.Info(cmd.OutOrStdout(), "...")
```

They prefix with colored Unicode symbols (✔ ⚠ ✘ 🛈). `--no-color` disables all color globally via `color.NoColor = true` in root's `PersistentPreRun`.

---

## Error Handling (`cmd/grafanactl/fail/`)

> See also [design-guide.md](design-guide.md) Section 4 for error design guidelines,
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
Error: Resource not found - code 404
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

### `ErrorToDetailedError` — Error Conversion Pipeline

`main.go:handleError` calls this on any error before printing. It runs a chain of type-specific converters:

```
ErrorToDetailedError(err)
    │
    ├─ errors.As(err, &DetailedError{}) → return as-is if already detailed
    ├─ convertConfigErrors   → ValidationError, UnmarshalError, ErrContextNotFound
    ├─ convertFSErrors       → fs.PathError (not exist, invalid, permission)
    ├─ convertResourcesErrors → InvalidSelectorError
    ├─ convertNetworkErrors  → url.Error
    ├─ convertAPIErrors      → k8s StatusError (401, 403, 404, ...)
    └─ fallback: DetailedError{Summary: "Unexpected error", Parent: err}
```

**Adding new error conversions:** add a `convertXxxErrors` function following the `func(error) (*DetailedError, bool)` signature and append it to the `errorConverters` slice in `ErrorToDetailedError`.

---

## How Config Flows Through Commands

`config.Options` is a reusable struct that bundles the `--config` and `--context` flags with three loading methods:

```
config.Options
├── BindFlags(flags)         — registers --config, --context flags
├── loadConfigTolerant(ctx)  — loads without full validation (config subcommands)
├── LoadConfig(ctx)          — loads + validates context + credentials
└── LoadRESTConfig(ctx)      — LoadConfig + constructs NamespacedRESTConfig
```

`resources.Command()` creates one `configOpts` instance, binds it to persistent flags on the group, then passes it by pointer into every subcommand constructor. Subcommands call `configOpts.LoadRESTConfig(ctx)` at execution time (not construction time), ensuring the flag values are already parsed.

---

## Convention: Adding a New `resources` Subcommand

**Step 1.** Create `cmd/grafanactl/resources/mycommand.go`.

**Step 2.** Follow the standard structure:

```go
package resources

import (
    cmdconfig "github.com/grafana/grafanactl/cmd/grafanactl/config"
    cmdio     "github.com/grafana/grafanactl/cmd/grafanactl/io"
    "github.com/spf13/cobra"
    "github.com/spf13/pflag"
)

type myOpts struct {
    IO      cmdio.Options   // include if command has --output flag
    OnError OnErrorMode     // include if command operates on multiple resources
    // ... command-specific fields
}

func (opts *myOpts) setup(flags *pflag.FlagSet) {
    // Register any custom output codecs BEFORE BindFlags.
    opts.IO.DefaultFormat("text")
    opts.IO.RegisterCustomCodec("text", &myTableCodec{})
    opts.IO.BindFlags(flags)

    bindOnErrorFlag(flags, &opts.OnError)  // if needed
    flags.StringVar(&opts.SomeField, "some-flag", "default", "description")
}

func (opts *myOpts) Validate() error {
    if err := opts.IO.Validate(); err != nil {
        return err
    }
    return opts.OnError.Validate()
}

func myCmd(configOpts *cmdconfig.Options) *cobra.Command {
    opts := &myOpts{}

    cmd := &cobra.Command{
        Use:     "mycommand [RESOURCE_SELECTOR]...",
        Args:    cobra.ArbitraryArgs,
        Short:   "One-liner description",
        Long:    "Longer description.",
        Example: "\n\tgrafanactl resources mycommand dashboards/foo",
        RunE: func(cmd *cobra.Command, args []string) error {
            ctx := cmd.Context()

            if err := opts.Validate(); err != nil {
                return err
            }

            cfg, err := configOpts.LoadRESTConfig(ctx)
            if err != nil {
                return err
            }

            // Use fetchResources if you need to read from Grafana:
            res, err := fetchResources(ctx, fetchRequest{Config: cfg}, args)
            if err != nil {
                return err
            }

            codec, err := opts.IO.Codec()
            if err != nil {
                return err
            }

            return codec.Encode(cmd.OutOrStdout(), res.Resources.ToUnstructuredList())
        },
    }

    opts.setup(cmd.Flags())
    return cmd
}
```

**Step 3.** Register in `resources/command.go`:

```go
cmd.AddCommand(myCmd(configOpts))
```

**Step 4.** No other wiring needed. Error handling, config loading, and logging are automatic.

---

## Key Invariants

| Rule | Location |
|---|---|
| `opts.Validate()` is the FIRST call in `RunE` | All resource commands |
| `configOpts.LoadRESTConfig` is called in `RunE`, not at construction | All resource commands |
| `--config` and `--context` are persistent on the group, not per-subcommand | `resources/command.go`, `config/command.go` |
| All errors bubble up through `RunE` return value; never `os.Exit` in commands | All commands |
| Status messages go to `cmd.OutOrStdout()`, not `os.Stdout` directly | All commands |
| Custom table codecs implement `format.Codec` and are registered before `BindFlags` | `get.go`, `list.go`, `validate.go` |
| Data fetching is format-agnostic — fetch all fields, let codecs filter display | All commands with custom codecs |
| `OnErrorMode` is always validated in `opts.Validate()`, not inline | All multi-resource commands |
| `terminal.Detect()` is called once in `PersistentPreRun`; use `terminal.IsPiped()` / `terminal.NoTruncate()` everywhere else | `root/command.go`, all table codecs |
| `--json` is mutually exclusive with `-o/--output`; enforced in `io.Options.Validate()` | `io/format.go` |
