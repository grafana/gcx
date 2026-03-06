# grafanactl: Project Structure and Build System

## 1. Directory Layout

```
grafanactl/
├── cmd/
│   └── grafanactl/           # Binary entry point (public surface)
│       ├── main.go           # Version vars, main(), error handler
│       ├── root/             # Root Cobra command, global flags, logging setup
│       ├── config/           # 'config' subcommand implementations
│       ├── resources/        # 'resources' subcommand implementations
│       ├── datasources/      # 'datasources' subcommand implementations
│       ├── dev/              # 'dev' subcommand (import, scaffold)
│       ├── query/            # 'query' subcommand implementation
│       ├── providers/        # 'providers' subcommand implementation
│       ├── fail/             # Error → DetailedError conversion, exit codes
│       └── io/               # Output formatting, user-facing messages
│
├── internal/                 # All non-public packages (Go enforced)
│   ├── config/               # Config loading, context management, auth types
│   │   └── testdata/         # YAML fixtures for config unit tests
│   ├── format/               # JSON/YAML codec, format auto-detection
│   ├── grafana/              # Thin wrapper over grafana-openapi-client-go
│   ├── graph/                # Terminal chart rendering (ntcharts + lipgloss)
│   ├── httputils/            # REST client helpers, request/response utilities
│   ├── logs/                 # slog + k8s klog integration, verbosity
│   ├── linter/               # OPA/Rego-based resource linter engine
│   │   ├── bundle/           # Embedded Rego bundle with built-in rules
│   │   └── builtins/         # Built-in PromQL/LogQL validators
│   ├── providers/            # Provider plugin system
│   │   ├── configloader.go   # Shared ConfigLoader for all providers
│   │   ├── alert/            # Alert provider (rules and groups)
│   │   └── slo/              # SLO provider implementation
│   │       ├── definitions/  # SLO definitions and status queries
│   │       └── reports/      # SLO reports
│   ├── query/                # Datasource query clients
│   │   ├── prometheus/       # Prometheus HTTP client (instant + range queries)
│   │   └── loki/             # Loki HTTP client (log + metric queries)
│   ├── secrets/              # Redaction of sensitive config fields
│   ├── testutils/            # Shared test helpers (not exposed externally)
│   ├── resources/            # Core resource abstraction layer
│   │   ├── discovery/        # API discovery: registry, index, preferred versions
│   │   ├── dynamic/          # k8s dynamic client wrapper (namespaced ops)
│   │   ├── local/            # FSReader / FSWriter (disk I/O)
│   │   ├── process/          # Processor pipeline (manager fields, server fields)
│   │   └── remote/           # Puller, Pusher, Deleter (Grafana API ops)
│   └── server/               # Local dev server for 'resources serve'
│       ├── embed/            # Static assets (embedded via go:embed)
│       ├── grafana/          # Grafana proxy and mock handlers
│       ├── handlers/         # Chi HTTP handler implementations
│       ├── livereload/       # WebSocket live reload broadcaster
│       └── watch/            # fsnotify file watcher integration
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
│   ├── integration-test-config.yaml  # grafanactl config pointing at localhost:3000
│   ├── default-config.yaml   # Default config fixture
│   └── folder.yaml           # Sample resource manifest
│
├── vendor/                   # Vendored Go dependencies (committed to repo)
├── bin/                      # Build output (gitignored)
├── build/                    # mkdocs output (gitignored)
│
├── Makefile                  # Unified build/test/lint/docs orchestration
├── go.mod / go.sum           # Go module definition (module: github.com/grafana/grafanactl)
├── .golangci.yaml            # Linter configuration (golangci-lint v2)
├── .goreleaser.yaml          # Release pipeline (cross-platform builds + GitHub Release)
├── devbox.json               # Reproducible toolchain (Go, golangci-lint, goreleaser, Python)
├── docker-compose.yml        # Integration test environment (Grafana 12 + MySQL 9)
├── mkdocs.yml                # Documentation site config (Material theme)
└── requirements.txt          # Python packages for mkdocs
```

### Rationale for cmd/ vs internal/ split

`cmd/grafanactl/` contains only the CLI wiring: flag parsing, command dispatch,
output formatting, and error translation. It holds no business logic.

`internal/` enforces Go's package visibility rule — external consumers cannot
import these packages. This is intentional: grafanactl has no public Go API.
The split within `internal/` mirrors functional layers (config, resources,
server) rather than technical concerns, making it easy to locate code by feature.

---

## 2. Build System (Makefile)

### Toolchain detection pattern

```makefile
ifneq "$(DEVBOX_CONFIG_DIR)" ""
    RUN_DEVBOX:=          # already inside devbox shell
else
    RUN_DEVBOX:=devbox run  # prefix every command with devbox run
endif
```

Every tool invocation is prefixed with `$(RUN_DEVBOX)`, so commands work
identically whether run directly inside `devbox shell` or from outside it.

### Key Makefile targets

| Target | What it does |
|---|---|
| `make all` | Runs lint + tests + build + docs (the full gate) |
| `make build` | Compiles `./cmd/grafanactl` into `bin/grafanactl` |
| `make install` | Copies binary to `$GOPATH/bin` |
| `make tests` | `go test -v ./...` (all packages, with race detection implied) |
| `make lint` | Runs `golangci-lint run -c .golangci.yaml` |
| `make deps` | `go mod vendor` + `pip install -r requirements.txt` |
| `make docs` | Runs `reference` then `mkdocs build` → `build/documentation/` |
| `make reference` | Runs all three doc-generation scripts |
| `make reference-drift` | Re-generates docs, fails if `git diff` finds changes |
| `make serve-docs` | `mkdocs serve` with live reload for doc development |
| `make test-env-up` | `docker-compose up -d` + health-wait loop |
| `make test-env-down` | `docker-compose down` |
| `make test-env-clean` | `docker-compose down -v` (removes volumes) |
| `make clean` | Removes `bin/`, `vendor/`, `.devbox/`, `.venv/` |

### Version injection

Version info is injected at link time via `-ldflags`:

```makefile
GIT_REVISION  ?= $(shell git rev-parse --short HEAD)
GIT_VERSION   ?= $(shell git describe --tags --exact-match 2>/dev/null || echo "")
BUILD_DATE    ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
VERSION_FLAGS := -X main.version=${GIT_VERSION} -X main.commit=${GIT_REVISION} -X main.date=${BUILD_DATE}
```

These set package-level `var` declarations in `cmd/grafanactl/main.go`:

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

## 3. Devbox (Reproducible Toolchain)

`devbox.json` pins the exact tool versions used across all environments:

```json
{
  "packages": [
    "go@1.26",
    "golangci-lint@2.9",
    "goreleaser@2.13.3",
    "python312@3.12.12"
  ],
  "shell": {
    "init_hook": [
      "echo 'Entering Python venv' && . $VENV_DIR/bin/activate",
      "echo 'Installing dependencies...' && make deps"
    ]
  }
}
```

The `init_hook` activates a Python virtualenv and runs `make deps` automatically
on `devbox shell`, so a new contributor gets a fully bootstrapped environment
from a single command. CI uses `jetify-com/devbox-install-action` to replicate
this exactly, pinned to `DEVBOX_VERSION: 0.16.0`.

---

## 4. CI/CD Pipeline (GitHub Actions)

Three workflow files under `.github/workflows/`:

### ci.yaml — Pull Request and Main Branch Gate

Triggered on: every PR and every push to `main`.

Three parallel jobs:

```
PR / push to main
├── linters  → make lint
├── tests    → make tests
└── docs     → make cli-reference (drift check) + make docs (build check)
```

All jobs:
1. Checkout with `persist-credentials: false` (minimal permissions)
2. Restore Go module cache keyed on `go.sum` hash
3. Install devbox (cached)
4. Run the Make target

Note: The CI `docs` job only runs `make cli-reference` for the drift check,
not all three reference generators. The env-var and config reference drift
checks are not currently part of CI (only `cli-reference-drift` is checked).

### release.yaml — Tag-Triggered Release

Triggered on: `v*` tag push.

```
v* tag push
├── release           → goreleaser release --clean  (builds + GitHub Release)
├── build_docs        → make docs → upload pages artifact
└── publish_docs      → deploy-pages action (needs: build_docs + release)
```

GoReleaser builds with `CGO_ENABLED=0` for all three platforms (linux, darwin,
windows) and creates:
- `tar.gz` archives for Linux/macOS (uname-compatible naming)
- `zip` archive for Windows
- `grafanactl_checksums.txt`

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
make reference
    ├── make cli-reference       → go run scripts/cmd-reference/*.go <outputDir>
    ├── make env-var-reference   → go run scripts/env-vars-reference/*.go <outputDir>
    └── make config-reference    → go run scripts/config-reference/*.go <outputDir>
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

```makefile
cli-reference-drift: cli-reference
    @if ! git diff --exit-code --quiet HEAD ./docs/reference/cli/ ; then
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
    ├── grafanactl-mysql (mysql:9.6)
    │   ├── Port: 3306
    │   ├── DB: grafana / User: grafana / Password: grafana
    │   └── healthcheck: mysqladmin ping
    └── grafanactl-grafana (grafana/grafana:12.3)
        ├── Port: 3000 (admin/admin)
        ├── DB: mysql (depends_on: mysql healthy)
        ├── Feature toggle: kubernetesDashboards=true  ← required for grafanactl
        ├── Config: ./testdata/grafana.ini (read-only mount)
        └── healthcheck: wget /api/health
```

The `kubernetesDashboards` feature toggle is essential — without it, the
Kubernetes-style API that grafanactl uses is not available in Grafana.

`testdata/integration-test-config.yaml` provides a ready-to-use grafanactl
config pointing at `localhost:3000` with `admin/admin` credentials and `org-id: 1`.

**Usage pattern for manual integration testing:**
```bash
make test-env-up
grafanactl --config testdata/integration-test-config.yaml resources list
make test-env-down
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
- **Output**: `build/documentation/` (via `make docs`)

Python dependencies pinned in `requirements.txt`:
```
mkdocs==1.6.1
mkdocs-material==9.7.1
mkdocs-material-extensions==1.3.1
mkdocs-nav-weight==0.3.0
```

These are installed via `pip install -r requirements.txt` into the devbox
Python venv during `make deps`. The site is deployed to GitHub Pages on release.

---

## 10. Quick Reference: How to Perform Common Tasks

### Build
```bash
make build                    # → bin/grafanactl
make install                  # → $GOPATH/bin/grafanactl
```

### Test and Lint
```bash
make tests                    # all unit tests
make lint                     # golangci-lint
make all                      # lint + tests + build + docs (full gate)
```

### Generate and Check Documentation
```bash
make reference                # regenerate all reference docs
make reference-drift          # fail if generated docs are stale
make docs                     # build full mkdocs site
make serve-docs               # live-reload doc server at localhost:8000
```

### Integration Testing (manual)
```bash
make test-env-up              # start Grafana + MySQL in Docker
grafanactl --config testdata/integration-test-config.yaml <command>
make test-env-down            # stop services
make test-env-clean           # stop + delete volumes
```

### Release (automated via CI on v* tag)
```bash
git tag v1.2.3 && git push --tags
# → release.yaml triggers goreleaser, publishes GitHub Release + GitHub Pages
```

### Add a New Dependency
```bash
go get github.com/some/package
make deps                     # runs go mod vendor to vendor new dep
git add vendor/ go.mod go.sum
```
