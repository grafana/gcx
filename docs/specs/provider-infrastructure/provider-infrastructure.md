# Wave 0: Provider Interface + Infrastructure — Implementation Plan

## Context

grafanactl currently only manages core Grafana resources (dashboards, folders) via the Kubernetes-compatible API. To support Grafana Cloud products (SLO, Synthetic Monitoring, OnCall, k6, ML), a pluggable provider system is needed. This wave establishes the foundational interface, registry, config extension, and CLI wiring that all subsequent provider implementations (Waves 1-3) will build on.

Source bead: `grafanactl-experiments-0s1`

## Requirements

**In scope:**
- `Provider` interface with `Name()`, `ShortDesc()`, `Commands()`, `Validate()`, `ConfigKeys()`
- Compile-time registry with `All() []Provider`
- Config extension: `Context.Providers map[string]map[string]string`
- Env var resolution: `GRAFANA_PROVIDER_{NAME}_{KEY}`
- Root command wiring for provider commands
- `grafanactl help providers` subcommand
- `docs/reference/provider-guide.md`

**Out of scope:**
- Actual product providers (SLO, SM, OnCall, k6, ML) — Waves 1-3
- API clients for Grafana Cloud products
- Provider-specific interactive setup
- Provider versioning/compatibility

**Acceptance criteria:**
1. `internal/providers/provider.go` defines `Provider` interface with `Name() string`, `ShortDesc() string`, `Commands() []*cobra.Command`, `Validate(cfg map[string]string) error`, `ConfigKeys() []string`
2. `internal/providers/registry.go` exports `All() []Provider` returning compile-time registered providers
3. `internal/config/types.go` includes `Providers map[string]map[string]string` in `Context` struct
4. Config loading resolves env vars `GRAFANA_PROVIDER_{NAME}_{KEY}` for provider config keys
5. `cmd/grafanactl/root/command.go` iterates `providers.All()` and adds provider commands to root
6. `grafanactl help providers` lists all registered providers with name and short description
7. `docs/reference/provider-guide.md` documents how to implement and register a new provider
8. `make lint && make tests` pass with no regressions

## Architecture

### Component Layout

```
internal/providers/
├── provider.go       # Provider interface definition
├── registry.go       # All() function, compile-time registration
└── provider_test.go  # Tests for registry and env var resolution

internal/config/
├── types.go          # + Providers field on Context
└── types_test.go     # + Tests for Providers serialization/validation

cmd/grafanactl/
├── root/command.go       # + Wire providers.All() into command tree
├── config/command.go     # + Env var resolution for provider config keys
└── help/
    └── providers.go      # "grafanactl help providers" subcommand

agent-docs/
└── provider-guide.md    # How to add a new provider
```

### Data Flow

```
Startup:
  providers.All()
       │
       ▼
  ┌──────────────┐     ┌────────────────────┐
  │ Registry     │────▶│ root/command.go     │
  │ []Provider   │     │ AddCommand(p.Cmds)  │
  └──────────────┘     └────────────────────┘

Config loading:
  YAML file  ──▶  config.Load()  ──▶  env.Parse(ctx)     [existing: Grafana fields]
                                           │
                                           ▼
                                  resolveProviderEnv()    [new: GRAFANA_PROVIDER_*]
                                           │
                                           ▼
                                  ctx.Providers map       [merged from file + env]

Help providers:
  grafanactl help providers  ──▶  providers.All()  ──▶  tabwriter table
                                   │
                                   ▼
                              Name() + ShortDesc() for each provider
```

### Key Design Decisions

| Decision | Options | Chosen | Why |
|----------|---------|--------|-----|
| Config field location | New struct vs. field on Context | Field on `Context` | Follows existing pattern; providers are per-context |
| Env var convention | `GRAFANA_{NAME}_{KEY}` vs. `GRAFANA_PROVIDER_{NAME}_{KEY}` | `GRAFANA_PROVIDER_{NAME}_{KEY}` | Namespaced, avoids collision with existing vars |
| Provider registration | `init()` + `Register()` vs. `All()` list | Explicit `All()` list | Compile-time visible, no init() side effects, testable |
| Help command location | `grafanactl providers list` vs. `grafanactl help providers` | `grafanactl help providers` | User requested; kubectl-style; discoverable |
| Provider commands in tree | Top-level per provider vs. grouped under `providers` | Top-level per provider | kubectl-style — `grafanactl slo list`, not `grafanactl providers slo list` |

## Implementation Tasks (4 tasks)

### Task 1: Provider interface + registry

- **Goal:** Define `Provider` interface and `All()` registry
- **Depends on:** —
- **Files:** `internal/providers/provider.go`, `internal/providers/registry.go`, `internal/providers/provider_test.go`
- **Deliverable:** `go build ./internal/providers/...` compiles; registry test passes with empty provider list
- **Verification:**
  - Unit test: `All()` returns empty slice
  - Unit test: mock provider satisfies interface
  - `make lint` passes

### Task 2: Config system extension

- **Goal:** Add `Providers map[string]map[string]string` to `Context`, wire env var resolution
- **Depends on:** Task 1 (needs `Provider` interface for `ConfigKeys()`)
- **Files:** `internal/config/types.go`, `internal/config/types_test.go`, `cmd/grafanactl/config/command.go`
- **Deliverable:** Config with `providers:` section round-trips through YAML; env vars `GRAFANA_PROVIDER_{NAME}_{KEY}` override config values; `config set`/`unset` works on provider keys
- **Verification:**
  - Unit test: YAML with `providers:` section deserializes to `Context.Providers`
  - Unit test: env var resolution merges provider config keys
  - `grafanactl config set contexts.default.providers.slo.token mytoken` works
  - `make tests` passes

### Task 3: Root command wiring + help providers

- **Goal:** Wire `providers.All()` into root command; add `grafanactl help providers`
- **Depends on:** Task 1
- **Files:** `cmd/grafanactl/root/command.go`, `cmd/grafanactl/help/providers.go`, `cmd/grafanactl/help/providers_test.go`
- **Deliverable:** Provider commands appear in root tree; `grafanactl help providers` prints a table of registered providers
- **Verification:**
  - Unit test: help providers command with mock registry renders table
  - With no providers registered: `grafanactl help providers` shows "No providers registered"
  - `make lint` passes

### Task 4: Provider guide documentation

- **Goal:** Write `docs/reference/provider-guide.md` documenting how to implement and register a new provider
- **Depends on:** Tasks 1, 2, 3 (needs final interface and config patterns)
- **Files:** `docs/reference/provider-guide.md`
- **Deliverable:** Complete guide with step-by-step instructions, code examples, config examples
- **Verification:**
  - Guide covers: interface implementation, registry registration, config keys, env vars, testing
  - Code examples compile (verified by inspection against interface)

## Task Dependency Graph

```
T1 (interface+registry) ──┬──→ T2 (config extension)  ──┐
                          │                               ├──→ T4 (provider guide)
                          └──→ T3 (wiring+help)    ──────┘
```

T2 and T3 can be done **in parallel** after T1. T4 depends on all three.

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|-----------|
| `caarlos0/env` can't handle dynamic provider keys | Medium | Custom env resolution step, tested independently |
| Reflection-based editor may not support nested maps | Medium | Test `config set contexts.x.providers.y.z val` early in T2 |
| Provider commands clash with existing command names | Low | Validate provider names against existing command tree at registration |
| Config migration for existing users | Low | Field is `omitempty` — existing configs unchanged |

## Verification Plan

**Automated checks:**
```bash
make lint     # golangci-lint
make tests    # go test -v ./...
make build    # Ensures binary compiles
```

**Manual smoke tests:**
1. `bin/grafanactl help providers` → shows "No providers registered" (or empty table)
2. `bin/grafanactl config set contexts.default.providers.slo.token mytoken` → config file updated
3. `bin/grafanactl config view` → shows providers section
4. `GRAFANA_PROVIDER_SLO_TOKEN=x bin/grafanactl config check` → no errors

**Edge cases:**
- Empty providers map (no providers registered)
- Provider with no config keys
- Env var set but no provider registered for that name
- Config file with unknown provider keys (preserved, not dropped)
