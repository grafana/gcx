# Constitution: gcx

> These are invariants. Violating them requires explicit human approval.

## Project Identity

**What it is:** Unified CLI for managing Grafana resources across two tiers — a K8s resource tier
for dashboards, folders, and other resources via Grafana 12+'s Kubernetes-compatible API, and a
Cloud provider tier with pluggable providers for Grafana Cloud products (SLO, Synthetic Monitoring,
OnCall, Fleet Management, etc.) using product-specific REST APIs.

**Primary values:** correctness, API stability, clean layered architecture, extensible provider model

## Architecture Invariants

- **Strict layer separation:** `cmd/` contains only CLI wiring (Cobra commands, flag parsing,
  output formatting) — no business logic. All domain logic lives in `internal/`.
- **Unstructured resource model:** Resources are `unstructured.Unstructured` objects — no
  pre-generated Go types. Dynamic discovery at runtime, not compile-time.
- **Folder-before-dashboard ordering:** Push pipeline does topological sort — folders are
  pushed level-by-level before any other resources.
- **Config follows kubeconfig pattern:** Named contexts with server/auth/namespace. Environment
  variable overrides follow the same precedence rules as kubectl.
- **Processor pipeline is composable:** Resource transformations use the `Processor` interface
  (`Process(*Resource) error`). Processors compose into ordered slices at defined pipeline points.
- **Format-agnostic data fetching:** Commands fetch all data regardless of `--output` format;
  codecs control display, not data acquisition.
- **Unified provider registration:** Each provider has exactly one `init()` function
  containing a single `providers.Register()` call. This atomically populates both the
  provider registry and the adapter registry — `providers.Register()` calls
  `adapter.Register()` for each entry returned by `Provider.TypedRegistrations()`.
  No separate `adapter.Register()` calls may exist outside `providers.Register()`.
- **ResourceIdentity on all domain types:** Every provider domain type used in a
  `ResourceAdapter` must implement `ResourceIdentity` (`GetResourceName() string` and
  `SetResourceName(string)`). `TypedCRUD` uses `GetResourceName()` for name extraction
  and `SetResourceName()` for name restoration — no function pointers.
- **TypedCRUD for provider commands:** Provider CRUD commands must use `TypedCRUD[T]`
  typed methods (`List`, `Get`, `Create`, `Update`, `Delete`) for data access, not raw
  API clients. This ensures bug fixes to CRUD logic apply to both provider commands and
  the `resources` pipeline automatically.
  > **Exception:** The dashboards commands-only provider (`internal/providers/dashboards/`) calls the K8s dynamic client directly. This is the one documented exception — see ADR 016 (`docs/adrs/dashboards-provider/001-dashboards-provider-design.md`) for rationale and scope.
- **Schema/Example on Registration structs:** Every `adapter.Registration` struct (populated
  via `TypedRegistrations()`) must include a non-nil `Schema` field. These power the
  `schemas` command via the global `SchemaForGVK`/`ExampleForGVK` functions — `AsAdapter()`
  does not propagate schema or example. The `Example` field MAY be nil for read-only
  resources (those without Create/Update support) since examples serve as templates for
  writable operations.

## CLI Grammar

- **Command structure follows `$AREA $NOUN $VERB`.** Resource and provider
  commands use `gcx {area} {resource-type} {verb}` (e.g.
  `gcx slo definitions list`, `gcx resources get`,
  `gcx logs query`). Tooling commands (`dev`, `config`)
  may use `$AREA $VERB` when there is no meaningful noun — these operate on
  the project or CLI itself, not on Grafana resources. Bare top-level
  verbs (single-token commands) are permitted only for foundational
  bootstrapping that precedes any area or resource context:
  `gcx login`, `gcx setup`, and Cobra-provided `help`/`completion`. This
  is an explicit, closed enumeration — any new top-level command must
  follow `$AREA $NOUN $VERB` or `$AREA $VERB`; it does not qualify as a
  bare verb by analogy.
- **Extension commands nest under their resource type.** Domain-specific
  operations (`status`, `timeline`, `acknowledge`) live alongside CRUD verbs,
  never as top-level commands. Extensions must not duplicate CRUD semantics —
  if it can be done with list/get/push/pull/delete, it is not an extension.
- **Positional arguments are the subject, flags are modifiers.** The thing
  being acted on (resource selectors, UIDs, expressions, file paths) is
  positional. How to act on it (output format, concurrency, dry-run, filters)
  is a flag.

## Dual-Purpose Design

Every command serves both humans and agents. Agent mode switches defaults
(JSON output, no color, no truncation) but does not change available
functionality. Explicit flags always override agent mode defaults.

See [agent-mode.md](docs/design/agent-mode.md) for
agent mode detection, behavior changes, and opt-out mechanisms.

- **All output goes through the codec system.** No command writes unstructured
  prose as its primary output. CRUD data commands output resources. CRUD
  mutation commands output structured operation summaries. Extension commands
  output domain-specific structured data.
- **Default output is proportional to what is actionable.** Mutation summaries
  enumerate exceptions (failures, skips) and summarize successes by count.
  Full per-resource detail is opt-in.
- **STDOUT is the result, STDERR is the diagnostic.** Summary tables and
  resource data go to stdout. Failure details and progress feedback go to
  stderr. Both use structured formats (tables or JSON), not unstructured prose.

## Push/Pull Philosophy

- **Local manifests are clean, portable, and environment-agnostic.** `pull`
  strips server-managed fields and writes a consistent format (default: YAML).
  `push` is idempotent (create-or-update) and treats local files as
  authoritative. The same manifests can be pushed to any Grafana instance
  via `--context` without modification.
- **Three workflows, one pipeline.** Whether used as source-of-truth (edit
  locally, push to Grafana), backup/rollback (pull periodically, push to
  restore), or migration/fanout (pull from source instance, push to targets),
  the push/pull pipeline is the same. The workflow differs only in triggering
  — manual, CI, or scheduled.
- **Folder-before-resource ordering** on push. Folders are topologically
  sorted by parent-child relationships and pushed level-by-level before
  any non-folder resources.

## Provider Architecture

- **Dual CRUD access paths are permanent.** Provider commands
  (`slo definitions list`) are ergonomic shorthands with domain-rich table
  output. Generic commands (`resources get slos.v1alpha1.slo.ext.grafana.app`)
  serve the push/pull pipeline and cross-resource operations. Neither path
  is deprecated; both are first-class.
- **JSON/YAML output is identical between both paths.** This is enforced
  structurally: provider CRUD commands must use their registered
  `ResourceAdapter` (via TypedCRUD) for data access, not raw API clients.
  Table/wide codecs may diverge — provider tables show domain-specific
  columns, generic tables show resource-management columns.
- **Provider-only resources must not mimic adapter verbs.** If a resource
  does not obey standard list/get/create/update/delete semantics (e.g.,
  composite keys, scope-required lookups, query-only endpoints), do not
  register it as an adapter. Keep it in the provider command tree only, but
  use alternative verbs (`show`, `describe`, `search`) — never `get`, `list`,
  `create`, `update`, `delete`. This avoids user confusion: adapter verbs
  (`resources get`) and provider verbs should not overlap for resources that
  behave differently across the two paths.
- **Sub-resources nest under their parent command.** If a resource cannot
  be listed or addressed without a parent ID (e.g. alerts require an
  alert group), it is a sub-resource. Sub-resources must not be registered as standalone typed
  adapters (no `ListFn` that ignores the parent). Instead, expose them
  as verbs under the parent command: `$PARENT $VERB-$CHILD $PARENT_ID`
  (e.g. `alert-groups list-alerts <id>`). Get-by-ID may still have a
  standalone adapter if the API supports direct ID lookup without a parent.
- **Typed resource trajectory.** Provider domain types implement
  `ResourceIdentity` for self-describing identity and are wrapped by
  `TypedObject[T]` (embedded `metav1.ObjectMeta` + `TypeMeta` + `Spec T`)
  for K8s metadata compliance. `TypedCRUD[T]` provides both typed methods
  (returning `TypedObject[T]`) and unstructured methods (via `AsAdapter()`).
  New providers must implement `ResourceIdentity` on domain types and use
  `TypedCRUD` for both CLI commands and adapter registration.

## Dependency Rules

- `cmd/` may import `internal/`; `internal/` may not import `cmd/`.
- No circular dependencies between packages.
- Provider implementations (`internal/providers/*/`) may import core resource types but not
  other providers.
- Query clients (`internal/query/*/`) bypass the k8s dynamic client — they call datasource
  HTTP APIs directly.
- PromQL construction uses `github.com/grafana/promql-builder/go/promql`, not string formatting.
- Provider implementations must use `providers.ConfigLoader` for config and
  auth resolution. Providers must not construct HTTP clients or load
  credentials independently — this ensures consistent env var precedence,
  secret handling, and auth behavior across all providers.
- **`httputils.NewDefaultClient(ctx)` for external APIs.** Provider clients
  calling APIs outside the Grafana server (k6 Cloud, OnCall, Synth, Fleet —
  any domain other than `cfg.Host`) must use `httputils.NewDefaultClient(ctx)`,
  never `rest.HTTPClientFor()`. The k8s transport round-tripper injects the
  Grafana bearer token on every outgoing request, which conflicts with the
  product's own auth mechanism. `NewDefaultClient(ctx)` returns an `*http.Client`
  with `LoggingRoundTripper` and no auth injection — providers set their own
  auth headers per request.
- **All HTTP clients via `httputils`.** Production code must create HTTP clients
  through `httputils.NewDefaultClient(ctx)` or `httputils.NewClient(ClientOpts{...})`.
  Bare `http.DefaultClient`, standalone `&http.Client{}`, and custom transports
  that bypass `LoggingRoundTripper` are forbidden. The K8s tier is exempt —
  it uses `rest.Config.WrapTransport` which chains `LoggingRoundTripper`
  via `config.NewNamespacedRESTConfig`. The `--log-http-payload` flag flows
  through context; `NewDefaultClient` reads it automatically.

## Taste Rules

- **Options pattern for every command:** opts struct + `setup(flags)` + `Validate()` + constructor.
- **Table-driven tests:** All Go tests follow [Go wiki conventions](https://go.dev/wiki/TableDrivenTests).
- **Commit format:** Title (one-liner) / What (description) / Why (rationale).
- **Error messages:** Lowercase, no trailing punctuation.
- **errgroup concurrency:** Bounded parallelism (default 10) for all batch I/O operations.

## Quality Standards

- All non-trivial functions have unit tests.
- `make all` (lint + tests + build + docs) must pass before merging.
- `GCX_AGENT_MODE=false make all` when running from agent environments
  (prevents agent-mode detection from altering doc generation).
- No linter suppressions without a comment explaining why.
- CI must pass before merging.
- **Architecture docs must stay current with code changes.** When adding or
  removing packages under `internal/` or `cmd/`, introducing new patterns,
  changing core abstractions, or adding a provider — update `docs/architecture/`
  using the structural checks in
  [docs/reference/doc-maintenance.md](docs/reference/doc-maintenance.md).
