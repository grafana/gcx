# gcx: Project Structure and Build System

## 1. Directory Layout

```
gcx/
├── cmd/
│   └── gcx/           # Binary entry point (public surface)
│       ├── main.go           # Version vars, main(), error handler
│       ├── root/             # Root Cobra command, global flags, logging setup
│       ├── auth/             # OAuth login command (browser-based PKCE flow)
│       ├── config/           # 'config' subcommand implementations
│       ├── resources/        # 'resources' subcommand implementations
│       ├── datasources/      # 'datasources' subcommand (list, get, query)
│       │   └── query/        # Auto-detecting query command (GenericCmd only)
│       ├── commands/         # 'commands' catalog (agent metadata, resource types, live validation)
│       ├── helptree/        # 'help-tree' compact text tree for agent context injection
│       ├── setup/            # 'setup' command area (onboarding, product config)
│       │   └── instrumentation/  # Instrumentation subcommands (status, discover, show, apply)
│       ├── skills/           # 'skills' subcommand (portable Agent Skills installer for .agents bundles)
│       ├── dev/              # 'dev' subcommand (import, scaffold, generate, lint, serve)
│       ├── providers/        # 'providers' subcommand implementation
│       └── fail/             # Error → DetailedError conversion, exit codes
│
├── internal/                 # All non-public packages (Go enforced)
│   ├── agent/                # Agent-mode detection, command annotations, known-resource registry with operation hints
│   ├── auth/                 # OAuth PKCE flow, token refresh transport
│   │   └── adaptive/         # Shared adaptive telemetry auth (GCOM caching, Basic auth)
│   ├── cloud/                # Grafana Cloud stack discovery via GCOM API
│   ├── fleet/                # Shared fleet base client (HTTP, auth, config — shared by fleet provider and setup/instrumentation)
│   ├── setup/
│   │   └── instrumentation/  # InstrumentationConfig manifest types, API client, optimistic lock comparison
│   ├── config/               # Config loading, context management, auth types
│   │   └── testdata/         # YAML fixtures for config unit tests
│   ├── format/               # JSON/YAML codec, format auto-detection
│   ├── output/               # Output codec registry (json, yaml, text, wide), field selection, user-facing messages
│   ├── grafana/              # Thin wrapper over grafana-openapi-client-go
│   ├── graph/                # Terminal chart rendering (ntcharts + lipgloss)
│   ├── httputils/            # REST client helpers, request/response utilities
│   ├── retry/                # Retry transport (429/5xx/connection errors, exponential backoff, Retry-After)
│   ├── logs/                 # slog + k8s klog integration, verbosity
│   ├── linter/               # OPA/Rego-based resource linter engine
│   │   ├── bundle/           # Embedded Rego bundle with built-in rules
│   │   └── builtins/         # Built-in PromQL/LogQL validators
│   ├── providers/            # Provider plugin system
│   │   ├── configloader.go   # Shared ConfigLoader for all providers
│   │   ├── metrics/          # Metrics signal provider (Prometheus queries + Adaptive Metrics)
│   │   │   └── adaptive/     # Adaptive Metrics commands (rules, recommendations)
│   │   ├── logs/             # Logs signal provider (Loki queries + Adaptive Logs)
│   │   │   └── adaptive/     # Adaptive Logs commands + TypedCRUD (patterns, exemptions, segments)
│   │   ├── traces/           # Traces signal provider (Tempo queries + Adaptive Traces)
│   │   │   └── adaptive/     # Adaptive Traces commands + TypedCRUD (policies, recommendations)
│   │   ├── profiles/         # Profiles signal provider (Pyroscope queries + adaptive stub)
│   │   ├── appo11y/          # App Observability provider (singleton config resources)
│   │   │   ├── overrides/    # MetricsGeneratorConfig with ETag concurrency
│   │   │   └── settings/     # PluginSettings
│   │   ├── alert/            # Alert provider (rules and groups)
│   │   ├── dashboards/       # Dashboards provider (CRUD, search, version history, snapshot) — CLI: `gcx dashboards`
│   │   │   ├── descriptor/   # Descriptor helpers (GVK, preferred version resolution)
│   │   │   ├── search/       # Full-text search via dashboard.grafana.app search endpoint
│   │   │   ├── snapshot/     # Snapshot rendering via Dashboard Image Renderer API
│   │   │   └── versions/     # Version history list + restore via dashboard.grafana.app
│   │   ├── faro/             # Frontend Observability provider (apps CRUD, sourcemaps sub-resource) — CLI: `gcx frontend`
│   │   ├── fleet/            # Fleet Management provider (pipeline and collector resources)
│   │   ├── incidents/        # IRM Incidents provider
│   │   ├── k6/              # k6 Cloud provider (projects, tests, runs, envvars)
│   │   ├── kg/               # Knowledge Graph (Asserts) provider
│   │   ├── oncall/           # OnCall provider (schedules, integrations, escalation chains)
│   │   ├── slo/              # SLO provider implementation
│   │   │   ├── definitions/  # SLO definitions and status queries
│   │   │   └── reports/      # SLO reports
│   │   └── synth/            # Synthetic Monitoring provider
│   │       ├── checks/       # Checks status, timeline, CRUD
│   │       ├── probes/       # Probe listing
│   │       └── smcfg/        # SM config loader interfaces
│   ├── deeplink/             # Deep link URL template registry and browser opener
│   ├── dashboards/           # Dashboard Image Renderer client (PNG snapshots)
│   ├── datasources/          # Datasource HTTP client (legacy REST API)
│   │   └── query/            # Shared query CLI utils (time parsing, codecs, opts, resolve helpers)
│   ├── query/                # Datasource query clients
│   │   ├── prometheus/       # Prometheus HTTP client (instant + range queries)
│   │   └── loki/             # Loki HTTP client (log + metric queries)
│   ├── notifier/             # Skills update notifier (XDG state, throttle, message rendering)
│   ├── secrets/              # Redaction of sensitive config fields
│   ├── skills/               # Portable Agent Skills installer primitives (Install, Update, Bundled/InstalledBundledSkillNames)
│   ├── terminal/             # TTY detection: IsPiped(), NoTruncate(), Detect()
│   ├── testutils/            # Shared test helpers (not exposed externally)
│   ├── resources/            # Core resource abstraction layer
│   │   ├── discovery/        # API discovery: registry, index, preferred versions
│   │   ├── dynamic/          # k8s dynamic client wrapper (namespaced ops)
│   │   ├── local/            # FSReader / FSWriter (disk I/O)
│   │   ├── process/          # Processor pipeline (manager fields, server fields)
│   │   └── remote/           # Puller, Pusher, Deleter (Grafana API ops)
│   ├── version/              # Global version string (Set once from main; provides UserAgent() for HTTP clients)
│   └── server/               # Local dev server for 'dev serve'
│       ├── embed/            # Static assets (embedded via go:embed)
│       ├── grafana/          # Grafana proxy and mock handlers
│       ├── handlers/         # Chi HTTP handler implementations
│       ├── livereload/       # WebSocket live reload broadcaster
│       └── watch/            # fsnotify file watcher integration
│   ├── shared/               # Shared utilities (date handling, duration, etc.) to be shared across integrations.
│
├── scripts/                  # Standalone Go programs for code generation
│   ├── cmd-reference/        # Generates CLI docs from Cobra tree
│   ├── config-reference/     # Generates config YAML reference from Go structs
│   ├── env-vars-reference/   # Generates env-var docs from struct tags
│   └── linter-rules-reference/  # Generates linter rule reference documentation
│
├── docs/                     # Documentation source (checked in)
│   ├── assets/               # Logo, custom CSS
│   ├── guides/               # Hand-written user guides
│   └── reference/            # Auto-generated reference pages (committed)
│       ├── cli/              # Per-command Markdown (from scripts/cmd-reference)
│       ├── configuration/    # Config YAML reference (from scripts/config-reference)
│       └── environment-variables/ # Env-var table (from scripts/env-vars-reference)
│
├── testdata/                 # Integration test fixtures (top-level)
│   ├── grafana.ini           # Grafana config for docker-compose Grafana service
│   ├── integration-test-config.yaml  # gcx config pointing at localhost:3000
│   ├── default-config.yaml   # Default config fixture
│   └── folder.yaml           # Sample resource manifest
│
├── vendor/                   # Vendored Go dependencies (committed to repo)
├── bin/                      # Build output (gitignored)
├── build/                    # mkdocs output (gitignored)
│
├── go.mod / go.sum           # Go module definition (module: github.com/grafana/gcx)
├── .golangci.yaml            # Linter configuration (golangci-lint v2)
├── .goreleaser.yaml          # Release pipeline (cross-platform builds + GitHub Release)
├── mise.toml                 # Reproducible toolchain (Go, golangci-lint, goreleaser, Python)
├── docker-compose.yml        # Integration test environment (Grafana 12 + MySQL 9)
├── mkdocs.yml                # Documentation site config (Material theme)
└── requirements.txt          # Python packages for mkdocs
```

### Rationale for cmd/ vs internal/ split

`cmd/gcx/` contains only the CLI wiring: flag parsing, command dispatch,
output formatting, and error translation. It holds no business logic.

`internal/` enforces Go's package visibility rule — external consumers cannot
import these packages. This is intentional: gcx has no public Go API.
The split within `internal/` mirrors functional layers (config, resources,
server) rather than technical concerns, making it easy to locate code by feature.

---

## 2. Build System (mise)

### Toolchain

Tools are managed by [mise](https://mise.jdx.dev/) via `mise.toml`. Once
`mise install` has been run, all tools (Go, golangci-lint, goreleaser, Python)
are available. All development commands use `mise run`, which ensures the correct
tool versions are used regardless of shell configuration.

### Key mise tasks

| Task | What it does |
|---|---|
| `mise run all` | Runs lint + tests + build + docs (the full gate) |
| `mise run build` | Compiles `./cmd/gcx` into `bin/gcx` |
| `mise run install` | Copies binary to `$GOPATH/bin` |
| `mise run tests` | `go test -v ./...` (all packages, with race detection implied) |
| `mise run lint` | Runs `golangci-lint run -c .golangci.yaml` |
| `mise run deps` | `go mod vendor` + `uv pip install -r requirements.txt` |
| `mise run docs` | Runs `reference` then `mkdocs build` → `build/documentation/` |
| `mise run reference` | Runs all four doc-generation scripts |
| `mise run reference-drift` | Re-generates docs, fails if `git diff` finds changes |
| `mise run serve-docs` | `mkdocs serve` with live reload for doc development |
| `mise run test-env-up` | `docker-compose up -d` + health-wait loop |
| `mise run test-env-down` | `docker-compose down` |
| `mise run test-env-clean` | `docker-compose down -v` (removes volumes) |
| `mise run clean` | Removes `bin/`, `vendor/`, `.venv/` |

### Version injection

Version info is injected at link time via `-ldflags`:

```bash
GIT_REVISION="$(git rev-parse --short HEAD)"
GIT_VERSION="$(git describe --tags --exact-match 2>/dev/null || echo "")"
BUILD_DATE="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
# passed via: -ldflags="-X main.version=... -X main.commit=... -X main.date=..."
```

These set package-level `var` declarations in `cmd/gcx/main.go`:

```go
var (
    version string  // "" → formatted as "SNAPSHOT" at runtime
    commit  string
    date    string
)
```

When no exact git tag matches, `GIT_VERSION` is empty and `formatVersion()`
substitutes `"SNAPSHOT"` at runtime, so development builds are clearly marked.

---

## 3. Mise (Reproducible Toolchain)

`mise.toml` pins the tool versions used across all environments:

```toml
[tools]
go = "1.26"
golangci-lint = "2.9"
goreleaser = "2.13.3"
python = "3.12"
uv = "latest"
```

A new contributor runs `mise install` to get the full toolchain, then `mise run deps`
to install Go vendors and Python packages. CI uses `jdx/mise-action` to replicate
this.

---

## 4. CI/CD Pipeline (GitHub Actions)

Three workflow files under `.github/workflows/`:

### ci.yaml — Pull Request and Main Branch Gate

Triggered on: every PR and every push to `main`.

Three parallel jobs:

```
PR / push to main
├── linters  → mise run lint
├── tests    → mise run tests
└── docs     → mise run reference-drift + mise run docs
```

All jobs:
1. Checkout with `persist-credentials: false` (minimal permissions)
2. Restore Go module cache keyed on `go.sum` hash
3. Install tools via mise (cached)
4. Run the mise task

### release.yaml — Tag-Triggered Release

Triggered on: `v*` tag push.

```
v* tag push
├── release           → goreleaser release --clean  (builds + GitHub Release)
├── build_docs        → mise run docs → upload pages artifact
└── publish_docs      → deploy-pages action (needs: build_docs + release)
```

GoReleaser builds with `CGO_ENABLED=0` for all three platforms (linux, darwin,
windows) and creates:
- `tar.gz` archives for Linux/macOS (uname-compatible naming)
- `zip` archive for Windows
- `gcx_checksums.txt`

The changelog is auto-generated from `git log` via GitHub, filtering out
`docs:`, `test:`, `tests:`, `chore:`, and merge commits.

Release concurrency is set to `cancel-in-progress: false` so in-flight releases
always complete.

### publish-docs.yaml — Manual Doc Deployment

Triggered on: `workflow_dispatch` only (manual trigger).

Used to republish documentation outside the normal release cadence without
cutting a new release. Follows the same build + upload + deploy pattern as
the release workflow.

---

## 5. Dependency Management

**Strategy: vendoring.** All dependencies are committed to `vendor/` and
`go mod vendor` is the canonical way to update them. The linter runs with
`modules-download-mode: vendor`, and the build uses vendored code.

**Rationale**: Vendoring ensures reproducible builds without a module proxy,
avoids network dependencies in CI, and makes the full dependency graph auditable
in code review.

### Dependency categories

| Category | Key packages | Purpose |
|---|---|---|
| Kubernetes client | `k8s.io/client-go`, `k8s.io/apimachinery`, `k8s.io/api`, `k8s.io/cli-runtime` | Dynamic client, GVK types, unstructured objects, discovery |
| Grafana libraries | `grafana/grafana-openapi-client-go`, `grafana/grafana/pkg/apimachinery`, `grafana/grafana-app-sdk/logging`, `grafana/authlib` | Generated Grafana API client, K8s extensions, structured logging |
| CLI framework | `spf13/cobra`, `spf13/pflag` | Subcommand tree, flag parsing |
| HTTP server | `go-chi/chi/v5`, `gorilla/websocket` | Serve command router, live reload WebSocket |
| Config / env | `caarlos0/env/v11`, `adrg/xdg` | Struct-based env-var parsing, XDG path resolution |
| Concurrency | `golang.org/x/sync` | `errgroup` for bounded parallel operations |
| YAML / JSON | `goccy/go-yaml`, `go-openapi/strfmt` | YAML codec, OpenAPI format types |
| File watching | `fsnotify/fsnotify` | Live reload file watcher |
| Terminal UI | `NimbleMarkets/ntcharts`, `charmbracelet/lipgloss` | Terminal chart rendering (bar charts, line graphs) |
| Terminal detection | `golang.org/x/term` | Terminal size detection for graph output |
| Testing | `stretchr/testify` | Assertions in unit tests |
| Semver | `Masterminds/semver/v3` | Version parsing/comparison |

---

## 6. Code Generation (scripts/)

All three generators are standalone `main` packages run via `go run`:

```
mise run reference
    ├── mise run reference:cli           → go run scripts/cmd-reference/*.go <outputDir>
    ├── mise run reference:env-var       → go run scripts/env-vars-reference/*.go <outputDir>
    ├── mise run reference:config        → go run scripts/config-reference/*.go <outputDir>
    └── mise run reference:linter-rules  → go run scripts/linter-rules-reference/*.go <outputDir>
```

### CLI Reference (`scripts/cmd-reference/main.go`)

Uses `github.com/spf13/cobra/doc.GenMarkdownTree` to walk the entire Cobra
command tree and emit one `.md` file per command into `docs/reference/cli/`.
The root command is instantiated with a fixed version string `"version"` since
the actual version is not relevant for documentation.

### Config Reference (`scripts/config-reference/main.go`)

Uses two techniques simultaneously:
1. **Go's `reflect` package** — walks `config.Config` struct fields recursively,
   reading `yaml:` struct tags to determine YAML key names
2. **Go's `go/parser` + `go/doc` packages** — parses `internal/config/` source
   files to extract GoDoc comments on struct types and fields

The output is a fully commented YAML skeleton showing every configuration key
with its type and documentation comment, written to
`docs/reference/configuration/index.md`.

### Env-Var Reference (`scripts/env-vars-reference/main.go`)

Same AST + reflect approach, but reads `env:` struct tags instead of `yaml:`
tags to discover all environment variable names. Emits a sorted Markdown
document to `docs/reference/environment-variables/index.md`.

### Drift Detection Pattern

```bash
# mise run reference-drift:cli runs reference:cli first, then:
if ! git diff --exit-code --quiet HEAD ./docs/reference/cli/ ; then
    echo "Drift detected..."
    exit 1
    fi
```

Generated docs are committed to the repo. CI re-generates them and uses
`git diff --exit-code` to fail if the output changed. This enforces that
generated docs always reflect the current code — developers must regenerate
and commit them when commands or config structs change.

---

## 7. Linting (golangci-lint v2)

`.golangci.yaml` uses `default: all` (opt-out model) and disables a curated
set of linters that conflict with the project's style:

**Disabled and why:**
- `cyclop`, `gocognit`, `funlen` — complexity metrics that would reject
  legitimately complex orchestration functions
- `lll` — line length (not enforced)
- `mnd` — magic number detection (too noisy for CLI tools)
- `exhaustruct` — requires all struct fields initialized (too verbose)
- `wrapcheck` — error wrapping consistency (flagged as low-priority debt)
- `paralleltest` — test parallelism enforcement (not currently required)
- `varnamelen`, `nlreturn`, `wsl`, `wsl_v5` — stylistic preferences not adopted

**Active formatters:**
- `gci` — import grouping order
- `gofmt` — standard Go formatting
- `goimports` — import management

**Notable settings:**
- `errcheck` excludes `fmt.*` functions (formatted print errors not checked)
- `depguard` denies `github.com/davecgh/go-spew` — debug statements must
  be removed before merging
- `revive`'s `var-naming` rule is disabled (allows non-standard naming)
- `modules-download-mode: vendor` — uses vendored deps, not module cache

---

## 8. Integration Test Infrastructure (docker-compose)

`docker-compose.yml` spins up a real Grafana 12 instance backed by MySQL 9:

```
docker-compose up -d
    ├── gcx-mysql (mysql:9.6)
    │   ├── Port: 3306
    │   ├── DB: grafana / User: grafana / Password: grafana
    │   └── healthcheck: mysqladmin ping
    └── gcx-grafana (grafana/grafana:12.3)
        ├── Port: 3000 (admin/admin)
        ├── DB: mysql (depends_on: mysql healthy)
        ├── Feature toggle: kubernetesDashboards=true  ← required for gcx
        ├── Config: ./testdata/grafana.ini (read-only mount)
        └── healthcheck: wget /api/health
```

The `kubernetesDashboards` feature toggle is essential — without it, the
Kubernetes-style API that gcx uses is not available in Grafana.

`testdata/integration-test-config.yaml` provides a ready-to-use gcx
config pointing at `localhost:3000` with `admin/admin` credentials and `org-id: 1`.

**Usage pattern for manual integration testing:**
```bash
mise run test-env-up
gcx --config testdata/integration-test-config.yaml resources schemas
mise run test-env-down
```

No automated integration tests currently exist — the docker-compose environment
is provided for manual developer testing only. This is identified as a gap
(see CLAUDE.md technical debt section).

---

## 9. Documentation Tooling (mkdocs)

`mkdocs.yml` configures a Material-theme static site:

- **Theme**: `material` with light/dark palette toggle
- **Plugins**: `search` + `mkdocs-nav-weight` (controls page ordering in nav)
- **Extensions**: `admonition`, `pymdownx.superfences` (code blocks),
  `pymdownx.tabbed` (tabbed content), `pymdownx.highlight` (syntax highlighting)
- **Output**: `build/documentation/` (via `mise run docs`)

Python dependencies pinned in `requirements.txt`:
```
mkdocs==1.6.1
mkdocs-material==9.7.1
mkdocs-material-extensions==1.3.1
mkdocs-nav-weight==0.3.0
```

These are installed via `uv pip install -r requirements.txt` during `mise run deps`.
The site is deployed to GitHub Pages on release.

---

## 10. Quick Reference: How to Perform Common Tasks

### Build
```bash
mise run build                # → bin/gcx
mise run install              # → $GOPATH/bin/gcx
```

### Test and Lint
```bash
mise run tests                # all unit tests
mise run lint                 # golangci-lint
mise run all                  # lint + tests + build + docs (full gate)
```

### Generate and Check Documentation
```bash
mise run reference            # regenerate all reference docs
mise run reference-drift      # fail if generated docs are stale
mise run docs                 # build full mkdocs site
mise run serve-docs           # live-reload doc server at localhost:8000
```

### Integration Testing (manual)
```bash
mise run test-env-up          # start Grafana + MySQL in Docker
gcx --config testdata/integration-test-config.yaml <command>
mise run test-env-down        # stop services
mise run test-env-clean       # stop + delete volumes
```

### Release (automated via CI on v* tag)
```bash
git tag v1.2.3 && git push --tags
# → release.yaml triggers goreleaser, publishes GitHub Release + GitHub Pages
```

### Add a New Dependency
```bash
go get github.com/some/package
mise run deps                 # runs go mod vendor to vendor new dep
git add vendor/ go.mod go.sum
```
