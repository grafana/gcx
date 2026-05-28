# Dependency Audit Report

Generated 2026-05-26. Analyses every direct dependency in go.mod for replacement feasibility.


## How to read this report

- **Files** = number of files in the codebase importing the package
- **Transitive deps** = number of dependencies the package itself pulls in (from `go mod graph`)
- **Unique transitive** = transitive deps NOT shared with any other direct dependency (what you'd actually save in go.sum)
- **Replace LOC** = estimated lines of code to hand-roll the used functionality
- **Verdict** = REMOVE (do it), CONSIDER (worth investigating), KEEP (not worth it)

---

## Tier 1: Zero-dependency leaf packages (easiest wins)

| Package | Files | Used API | Transitive | Unique | Replace LOC | Verdict | PR |
|---------|-------|----------|------------|--------|-------------|---------|-----|
| `huandu/xstrings` | 5 | `ToKebabCase`, `ToSnakeCase`, `ToPascalCase` | 0 | 0 | ~35 | **REMOVE** | [#779](https://github.com/grafana/gcx/pull/779) |
| `caarlos0/env/v11` | 1 | `env.Parse()` | 0 | 0 | ~45 | **REMOVE** | [#779](https://github.com/grafana/gcx/pull/779) |
| `google/uuid` | 5 | `uuid.New().String()`, `uuid.Parse()` | 0 | 0 | ~20 | **REMOVE** | [#779](https://github.com/grafana/gcx/pull/779) |
| `pkg/browser` | 5 | `browser.OpenURL()` | 1 | 0 | ~20 | **REMOVE** | [#779](https://github.com/grafana/gcx/pull/779) |
| `go-logr/logr` | 5 | Bridging only (klog adapter) | 0 | 0 | 0 | **REMOVE** (transitive of klog) | — |

### Details

- **xstrings**: 3 trivial case-conversion functions used in scaffold/import/generate. A ~35 line `internal/strconv` package replaces it completely. Zero transitive deps.
- **caarlos0/env**: Used in exactly one file (`internal/config/envparse.go`) to call `env.Parse()`. The wrapper functions `PrepareForEnvParse`/`CleanupAfterEnvParse` already exist to work around library limitations. ~45 lines of reflection code replaces it.
- **google/uuid**: 4 calls to `uuid.New().String()` for request IDs, 1 call to `uuid.Parse()`. Replace with `crypto/rand` for generation and format validation for parsing.
- **pkg/browser**: Single function `OpenURL()`. Replace with `exec.Command("open", url)` on macOS, `xdg-open` on Linux, `cmd /c start` on Windows. ~20 lines with runtime.GOOS switch.
- **go-logr/logr**: Not directly used in business logic. It's a bridging dependency for klog. Required as long as klog exists.

---

## Tier 2: Small packages with some transitive deps

| Package | Files | Used API | Transitive | Unique | Replace LOC | Verdict | PR |
|---------|-------|----------|------------|--------|-------------|---------|-----|
| `adrg/xdg` | 4 | `ConfigHome`, `StateHome`, `ConfigDirs`, `ConfigFile()`, `Reload()` | 5 | 0 | ~25 | **REMOVE** | [#779](https://github.com/grafana/gcx/pull/779) |
| `gofrs/flock` | 2 | `New()`, `TryLockContext()`, `Unlock()` | 8 | 0 | ~30 | **REMOVE** | [#779](https://github.com/grafana/gcx/pull/779) |
| `go-logfmt/logfmt` | 1 | `NewDecoder`, `ScanRecord`, `ScanKeyval`, `Key`, `Value` | 2 | 0 | ~110 | **CONSIDER** | — |
| `golang.org/x/mod` | 1 | `module.PseudoVersionRev()`, `PseudoVersionTime()` | 2 | 0 | ~30 | **KEEP** | — |
| `golang.org/x/term` | 4 | `term.IsTerminal()`, `term.GetSize()` | 2 | 0 | ~40 | **KEEP** | — |
| `Masterminds/semver/v3` | 3 | `NewVersion`, `GreaterThan`, `Major`, `String` | 1 | 0 | ~70 | **CONSIDER** | — |
| `google/go-cmp` | 5 | `cmp.Diff()` (test/linter only) | 1 | 0 | ~30 | **CONSIDER** | — |
| `fsnotify/fsnotify` | 1 | `NewWatcher`, `Add`, `Events`, `Errors`, `Has` | 2 | 0 | polling: ~40 | **CONSIDER** | — |

### Details

- **xdg**: Read `$XDG_CONFIG_HOME` or default to `~/.config`, same for `$XDG_STATE_HOME`. ~25 lines of env var lookups. No unique transitive deps.
- **gofrs/flock**: File locking used in 2 files for OAuth token persistence. Replace with `syscall.Flock()` on Unix. ~30 lines. No unique transitive deps.
- **go-logfmt**: Used in 1 file for Loki log formatting. Logfmt parsing requires careful quote handling. ~110 lines is doable but not trivial.
- **x/mod**: 2 functions for pseudo-version parsing. Well-maintained stdlib extension, stays as indirect dep anyway. Not worth replacing.
- **x/term**: `IsTerminal` and `GetSize`. Well-maintained stdlib extension, stays as indirect dep anyway. Not worth replacing.
- **semver**: Version parsing and comparison. Straightforward but pre-release/metadata handling adds complexity. ~70 lines for the subset used.
- **go-cmp**: `cmp.Diff()` in test/linter code. Replace with `reflect.DeepEqual` + manual diff printing. ~30 lines but less useful diff output.
- **fsnotify**: Used only for dev server livereload. Could switch to polling (~40 lines) at the cost of CPU. Only matters for `gcx dev serve`.

---

## Tier 3: Medium packages - evaluate case by case

| Package | Files | Used API | Transitive | Unique | Replace LOC | Verdict |
|---------|-------|----------|------------|--------|-------------|---------|
| `fatih/color` | 3 | `NoColor`, `New()`, `Fg*`, `Bold`, `Faint`, `SprintfFunc()` | 4 | 0 | ~70 | **CONSIDER** |
| `gorilla/websocket` | 2 | `Upgrader`, `Conn`, `ReadMessage`, `WriteMessage`, `Close` | 1 | 0 | ~150 | **KEEP** |
| `invopop/jsonschema` | 1 | `Reflector`, `Reflect()`, `DoNotReference` | 9 | 0 | ~170 | **KEEP** |
| `go-openapi/runtime` | 1 | `runtime.APIError` (type assertion) | 37 | 0 | ~20 | **REMOVE** (if grafana-openapi-client-go removed) |
| `go-openapi/strfmt` | 1 | `strfmt.Default` | 8 | 0 | ~10 | **REMOVE** (if grafana-openapi-client-go removed) |
| `grafana/authlib/types` | 4 | `ParseNamespace`, `CloudNamespaceFormatter`, `OrgNamespaceFormatter` | 8 | 0 | ~35 | **CONSIDER** |
| `grafana/grafana/apps/folder` | 1 | `FolderKind().Group()`, `FolderKind().Kind()` | 58 | 0 | ~10 | **CONSIDER** |
| `golang.org/x/sync` | 91 | `errgroup.Group`, `WithContext`, `Go`, `Wait`, `SetLimit` | 1 | 0 | ~100 | **KEEP** (91 files) |
| `golang.org/x/tools` | 1 | `imports.Process()` | 8 | 2 | ~250 | **CONSIDER** |
| `k8s.io/klog/v2` | 1 | `SetLoggerWithOptions()`, `ContextualLogger()` | 2 | 0 | **KEEP** (required by client-go) |
| `sigs.k8s.io/yaml` | 4 | `JSONToYAML()` | 5 | 0 | ~50 | **CONSIDER** |

### Notable highlights

- **go-openapi/runtime + strfmt**: Only needed because of `grafana-openapi-client-go`. If you replaced the OpenAPI client with direct HTTP calls (it's only used for health checks), you'd drop all three packages and their ~37 unique transitive deps.
- **grafana/apps/folder**: Imported for 2 constant string comparisons (`Group()` and `Kind()`). Could be replaced with `const folderGroup = "folder.grafana.app"` etc. But the 58 transitive deps are shared with other grafana/* packages.
- **x/tools**: Used in exactly 1 file (`cmd/gcx/dev/import.go`) for `imports.Process()` which auto-manages Go imports. Has 2 unique transitive deps. Could be dropped if you accept unformatted Go output (there's already a fallback).
- **x/sync**: Used in 91 files. Not worth replacing despite being ~100 LOC - the migration cost outweighs the benefit.

---

## Tier 4: Heavy framework deps (keep)

| Package | Files | Transitive | Unique | Verdict | Why |
|---------|-------|------------|--------|---------|-----|
| `spf13/cobra` | 730 | 4 | 0 | **KEEP** | CLI framework backbone |
| `spf13/pflag` | 440 | 0 | 0 | **KEEP** | Tightly coupled to Cobra |
| `stretchr/testify` | 802 | 11 | 0 | **KEEP** | Test-only, 802 files - migration not worth it |
| `k8s.io/apimachinery` | 35+ | 39 | 0 | **KEEP** | Core K8s type system |
| `k8s.io/client-go` | 20+ | 49 | 0 | **KEEP** | K8s API client foundation |
| `k8s.io/cli-runtime` | 1 | 57 | 7 | **CONSIDER** | Only `printers.TablePrinter` used. 7 unique transitive deps. |
| `charmbracelet/lipgloss` | 28 | 14 | 0 | **KEEP** | Deep styling integration |
| `charmbracelet/huh` | 6 | 33 | 8 | **KEEP** | Terminal forms UI complexity |
| `charmbracelet/glamour` | 1 | 28 | 6 | **CONSIDER** | Only `Render()` used. 6 unique transitive deps. |
| `goccy/go-yaml` | 34 | 1 | 0 | **KEEP** | Heavy usage, already alternate to yaml.v3 |
| `gopkg.in/yaml.v3` | 30 | 2 | 0 | **KEEP** | Could consolidate to goccy but not removable |
| `grafana/grafana-app-sdk/logging` | 204 | 8 | 0 | **KEEP** | 204-file migration outweighs ~100 LOC benefit |
| `grafana/grafana-foundation-sdk/go` | 1 | 1 | 0 | **KEEP** | Domain-specific codegen types |
| `grafana/grafana-openapi-client-go` | 2 | 37 | 27 | **CONSIDER** | Only health check + login validation. 27 unique deps! |
| `grafana/grafana/pkg/apimachinery` | 35 | 52 | 0 | **KEEP** | Metadata constants, accessor interface |
| `grafana/promql-builder/go` | 19 | 1 | 0 | **KEEP** | Type-safe PromQL construction |
| `olekukonko/tablewriter` | 1 | 13 | 3 | **CONSIDER** | Only linter reporter. 3 unique transitive deps. |
| `NimbleMarkets/ntcharts` | 1 | 22 | 0 | **KEEP** | Terminal charting complexity |
| `open-policy-agent/opa` | 29 | 121 | 46 | **KEEP** | Full Rego policy engine, rules are in Rego |
| `prometheus/prometheus` | 5 | 249 | 164 | **CONSIDER** | Only PromQL parser. 164 unique transitive deps! |
| `google.golang.org/protobuf` | 1 direct | 3 | 0 | **KEEP** | Required by K8s deps |

---

## Highest-impact removal candidates (sorted by value)

| # | Package | Replace LOC | Unique transitive deps saved | Files to touch | Difficulty | PR |
|---|---------|------------|------------------------------|----------------|------------|-----|
| 1 | **prometheus/prometheus** | ~150 (PromQL parser) | **164** | 5 | Hard - need PromQL parser alternative | — |
| 2 | **grafana-openapi-client-go** (+runtime, strfmt) | ~150 (HTTP client) | **~27** | 6 | Medium - rewrite as direct HTTP calls | — |
| 3 | **k8s.io/cli-runtime** | ~120 (table printer) | **7** | 1 | Easy - extend existing table logic | — |
| 4 | **charmbracelet/glamour** | ~40 (strip markdown) | **6** | 1 | Easy - basic markdown stripping | — |
| 5 | **huandu/xstrings** | ~35 | **0** | 5 | Trivial | [#779](https://github.com/grafana/gcx/pull/779) |
| 6 | **caarlos0/env** | ~45 | **0** | 1 | Trivial | [#779](https://github.com/grafana/gcx/pull/779) |
| 7 | **pkg/browser** | ~20 | **0** | 5 | Trivial | [#779](https://github.com/grafana/gcx/pull/779) |
| 8 | **google/uuid** | ~20 | **0** | 5 | Trivial | [#779](https://github.com/grafana/gcx/pull/779) |
| 9 | **adrg/xdg** | ~25 | **0** | 4 | Trivial | [#779](https://github.com/grafana/gcx/pull/779) |
| 10 | **gofrs/flock** | ~30 | **0** | 2 | Trivial | [#779](https://github.com/grafana/gcx/pull/779) |
| 11 | **x/mod** | ~30 | **0** | 1 | Trivial | — |
| 12 | **x/term** | ~40 | **0** | 1 | Trivial | — |
| 13 | **olekukonko/tablewriter** | ~80 | **3** | 1 | Easy | — |

---

## Big-ticket items worth a deeper look

### `prometheus/prometheus` (164 unique transitive deps!)

Only `promql.NewParser(opts).ParseExpr(expr)` is used to validate PromQL expressions in the linter. The Prometheus module pulls in 249 transitive deps, of which 164 are unique to it. This is by far the heaviest dependency for the least API surface used.

Options:
1. Use a standalone PromQL parser library (if one existed as a separate module)
2. Shell out to `promtool` for validation
3. Write a basic PromQL syntax validator (~150 LOC for the subset actually validated)
4. Accept unvalidated PromQL in the linter and rely on Grafana/Prometheus to reject invalid queries at runtime

### `grafana-openapi-client-go` (27 unique transitive deps)

Used in 2 files for health checks and login validation. The generated OpenAPI client pulls in the entire go-openapi stack (analysis, loads, spec, validate, etc.) - 27 unique transitive deps. Replacing with direct `net/http` calls to `/api/health` and version parsing would be ~150 lines and much lighter.

---

## Grafana-internal dependencies

| Package | Files | Used API | Assessment |
|---------|-------|----------|------------|
| `grafana/authlib/types` | 4 | `ParseNamespace`, `CloudNamespaceFormatter`, `OrgNamespaceFormatter` | Trivial to replace (~35 LOC) but Grafana-internal |
| `grafana/grafana-app-sdk/logging` | 204 | `FromContext`, `NewSLogLogger`, `Context`, `Logger` | Thin slog wrapper, 204-file migration cost |
| `grafana/grafana-foundation-sdk/go` | 1 | Dashboard/Folder converters | Impractical - auto-generated domain types |
| `grafana/grafana-openapi-client-go` | 2 | `NewHTTPClientWithConfig`, `GetHealth` | Only health checks - worth replacing with HTTP |
| `grafana/grafana/apps/folder` | 1 | `FolderKind().Group()`, `.Kind()` | 2 string constants (~10 LOC) |
| `grafana/grafana/pkg/apimachinery` | 35 | `MetaAccessor`, annotation constants | Moderate - 150-250 LOC |
| `grafana/promql-builder/go` | 19 | Fluent PromQL builder | Moderate - type-safe DSL |
