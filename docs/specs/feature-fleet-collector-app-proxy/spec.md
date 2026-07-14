---
type: feature-spec
title: "Route `fleet` and `instrumentation` through the collector-app plugin proxy"
status: done
created: 2026-07-07
---

# Route `fleet` and `instrumentation` through the collector-app plugin proxy

## Problem Statement

`gcx fleet` and `gcx instrumentation` talk to the Grafana Fleet Management (FM)
API directly. They share the base HTTP client in `internal/fleet/`, which POSTs
Connect-style JSON with HTTP Basic auth — the FM instance ID as username and an
**FM-scoped Cloud access-policy token** as password. Configuration for that path
is resolved through `providers.ConfigLoader.LoadCloudConfig`, which hard-requires
`cloud.token` (it fails with `cloud token is required` when absent) and reads
`AgentManagementInstanceURL` / `AgentManagementInstanceID` from GCOM stack info.

Who is affected: every operator or CI job that wants to drive fleet or
instrumentation commands. FM's direct API does not accept a Grafana OAuth
credential, so interactive OAuth users are blocked outright — they can
authenticate to Grafana but cannot obtain the FM-scoped token these commands
demand.

Current workaround: users must provision and carry a second, FM-scoped Cloud
access-policy token alongside their Grafana credential — the two-tier "Grafana
credential *and* FM token" requirement. There is no workaround at all for a
pure-OAuth context.

The `grafana-collector-app` app plugin (which the Fleet Management UI already
uses) exposes the same FM routes under `/fleet-management-api/...` behind Grafana
authNZ. Routing gcx's fleet/instrumentation traffic through that plugin proxy at
`cfg.Host` lets any Grafana credential — OAuth included — drive these commands,
converges them onto gcx's dominant plugin-proxy transport
(assistant/aio11y/irm/k6/slo/kg), and removes the Cloud-token requirement. This
decision is recorded and accepted in ADR 001.

## Scope

### In Scope

- Swapping the `internal/fleet/` base client transport from direct FM Basic-auth
  POST to the `grafana-collector-app` app plugin-proxy pattern at `cfg.Host`,
  using `rest.HTTPClientFor(&cfg.Config)`.
- Resolving connection details for both command groups via
  `providers.ConfigLoader.LoadGrafanaConfig(ctx)` instead of `LoadCloudConfig`.
- Widening the `internal/fleet` `ConfigLoader` interface to expose
  `LoadGrafanaConfig`, and reworking `LoadClient` / `LoadClientWithStack`.
- Dropping the FM access-policy token and the `AgentManagementInstanceURL` /
  `AgentManagementInstanceID` GCOM lookups from the fleet load path.
- Dropping client-side `X-Prom-Cluster-ID` / `X-Prom-Instance-ID` and
  `X-Scope-OrgID` request decoration in `internal/providers/instrumentation/`,
  and removing the now-vestigial `PromHeaders` plumbing threaded through the
  `cmd/gcx/instrumentation/**` call sites.
- Re-sourcing `instrumentation`'s backend datasource URLs (Mimir/Loki/Tempo/
  Pyroscope) via the collector-app's Grafana-authenticated instance-metadata
  proxy route (Viewer role) so no Cloud token is required, while keeping those
  URLs in the request body.
- Amending `CONSTITUTION.md` to remove **Fleet** from the "APIs outside the
  Grafana server" dependency rule and the Project Identity intro naming.
- Stating the Grafana role requirement (Viewer for reads, Grafana Admin for
  mutations) in the `gcx fleet` / `gcx instrumentation` Cobra help and
  regenerating the auto-generated CLI reference.
- Reconciling tests in `internal/fleet`, `internal/providers/fleet`, and
  `internal/providers/instrumentation` to the new constructor signature and
  auth expectations.
- Error mapping for proxy/RBAC responses so failures stay actionable.

### Out of Scope

- Changing typed method surfaces (`ListPipelines`, `*Pipeline`, `*Collector`,
  `GetLimits`, `Get/Set` `App`/`K8S` instrumentation, `SetupK8sDiscovery`,
  `RunK8sDiscovery`, `RunK8sMonitoring`) or the Connect service/method path
  suffixes — these remain byte-for-byte unchanged.
- Changing the Connect JSON wire format of request or response bodies.
- Implementing fine-grained per-resource RBAC in the plugin — the proxy today
  gates on Viewer/Admin roles only; finer authorization is a future plugin-side
  enhancement (see Open Questions).
- Any hybrid or fallback transport that retains direct FM access.
- Documenting `grafana-collector-app` plugin internals (the public gcx repo
  describes only gcx's own proxy calls).
- Changing behaviour of any other provider that already uses the plugin-proxy
  transport.

## Key Decisions

| Decision | Chosen | Rationale | Source |
|---|---|---|---|
| Transport for fleet/instrumentation FM traffic | `grafana-collector-app` app plugin-proxy at `cfg.Host` via `rest.HTTPClientFor(&cfg.Config)` | Grafana authNZ works for any credential (OAuth included); converges onto gcx's dominant transport family | ADR 001 |
| Proxy path prefix | `/api/plugin-proxy/grafana-collector-app/...` (app plugin-proxy), NOT `/api/plugins/<id>/resources/...` | collector-app is frontend-only and ships no backend, so the backend-resources endpoint would not resolve | ADR 001 |
| Config resolution | `LoadGrafanaConfig(ctx) → config.NamespacedRESTConfig` | Grafana-auth config already plumbed; removes the `cloud.token` hard requirement in `LoadCloudConfig`/`loadCloudBase` | ADR 001 |
| Cloud/FM token requirement | Removed for both command groups | Proxy authenticates the caller to Grafana, not to FM; instrumentation backend URLs re-sourced via a Grafana-auth proxy route | ADR 001 |
| Instrumentation backend-URL source | collector-app Grafana-authenticated instance-metadata proxy route (Viewer role) | Removes the residual direct-GCOM/Cloud-token dependency, keeping instrumentation fully OAuth-capable | ADR 001 |
| Cutover strategy | Clean cutover, no hybrid/fallback | Commands are Cloud-only and the plugin is guaranteed on all Cloud stacks, so a fallback branch would serve no real environment | ADR 001 |
| CONSTITUTION dependency rule | Amend to remove Fleet from the "outside the Grafana server" list | The traffic now targets `cfg.Host`, where `rest.HTTPClientFor()` is the correct client — the inverse of the current rule | ADR 001 |
| Role gating | Viewer for reads (`List*`/`Get*`); Grafana Admin for all mutations (plus `GetLimits` and the instrumentation/discovery calls) | Matches the plugin's role-level authorization | ADR 001 |

## Functional Requirements

- **FR-001** — The `internal/fleet` base client MUST build its HTTP client via
  `rest.HTTPClientFor(&cfg.Config)` from a `config.NamespacedRESTConfig` and MUST
  target `cfg.Host + /api/plugin-proxy/grafana-collector-app/fleet-management-api`
  concatenated with the existing Connect service/method path.
- **FR-002** — The `internal/fleet` base client constructor MUST drop the
  `instanceID`, `apiToken`, and `useBasicAuth` parameters.
- **FR-003** — The base client MUST NOT set an `Authorization` header or Basic
  auth itself; the Grafana bearer credential MUST be injected by the k8s
  round-tripper obtained from `rest.HTTPClientFor`.
- **FR-004** — Both command groups MUST resolve connection details via
  `providers.ConfigLoader.LoadGrafanaConfig(ctx)` returning
  `config.NamespacedRESTConfig`, and MUST NOT call `LoadCloudConfig` for fleet or
  instrumentation transport.
- **FR-005** — The `internal/fleet` `ConfigLoader` interface MUST be widened to
  declare `LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig,
  error)`, and `LoadClient` / `LoadClientWithStack` MUST build the plugin-proxy
  client from that `NamespacedRESTConfig`.
- **FR-006** — The fleet load path MUST NOT read `AgentManagementInstanceURL`,
  `AgentManagementInstanceID`, or an FM access-policy token, and MUST NOT emit the
  `fleet management endpoint is not available` / `instance ID is not available`
  errors that gate on those fields.
- **FR-007** — The instrumentation client MUST NOT set the `X-Prom-Cluster-ID`,
  `X-Prom-Instance-ID`, or `X-Scope-OrgID` headers; these are injected server-side
  by the proxy.
- **FR-008** — The `PromHeaders` type, `PromHeadersFromStack`, `toMap`, and the
  `PromHeaders` parameter threaded through `DoRequestWithHeaders` and the
  `cmd/gcx/instrumentation/**` call sites MUST be removed.
- **FR-009** — The instrumentation command group MUST source backend datasource
  URLs (Mimir/Loki/Tempo/Pyroscope) via the collector-app's Grafana-authenticated
  instance-metadata proxy route (Viewer role), NOT via a direct GCOM call or a
  Cloud access-policy token.
- **FR-010** — Backend datasource URLs MUST continue to travel in the
  instrumentation `Set*` / `Setup*` request bodies unchanged; only their source
  moves.
- **FR-011** — Running any `gcx fleet` or `gcx instrumentation` command MUST NOT
  require `cloud.token`, `GRAFANA_CLOUD_TOKEN`, or an FM-scoped access-policy
  token.
- **FR-012** — The typed pipeline/collector/tenant/instrumentation/discovery
  method surfaces and their Connect service/method path suffixes MUST remain
  unchanged.
- **FR-013** — `CONSTITUTION.md` MUST be amended to remove **Fleet** from the
  `httputils.NewDefaultClient(ctx)` "APIs outside the Grafana server" dependency
  rule and from the Project Identity intro naming, and MUST document that
  fleet/instrumentation reach FM via the collector-app proxy at `cfg.Host` using
  `rest.HTTPClientFor()`.
- **FR-014** — The `gcx fleet` and `gcx instrumentation` Cobra `Short`/`Long`
  help MUST state the Grafana role requirement: Viewer for reads, Grafana Admin
  for mutations.
- **FR-015** — The auto-generated CLI reference (`docs/reference/cli/gcx_fleet*.md`,
  `gcx_instrumentation*.md`) MUST be regenerated via
  `GCX_AGENT_MODE=false mise run reference` so `mise run reference-drift` passes.
- **FR-016** — Proxy/RBAC error responses MUST be mapped to actionable messages
  that distinguish an insufficient-role/RBAC denial from an FM rejection of the
  request.
- **FR-017** — Tests in `internal/fleet`, `internal/providers/fleet`, and
  `internal/providers/instrumentation` MUST be updated to the new constructor
  signature (no `useBasicAuth`/instance-ID/token) and MUST assert the plugin-proxy
  path/URL and bearer-injected transport instead of Basic auth.
- **FR-018** — The implementation MUST NOT provide any hybrid or fallback path to
  direct FM access; a single plugin-proxy transport MUST serve all fleet and
  instrumentation FM traffic.

## Acceptance Criteria

- GIVEN a context authenticated to Grafana by OAuth with no `cloud.token`,
  `GRAFANA_CLOUD_TOKEN`, or FM access-policy token, and the caller has the Viewer
  role
  WHEN the operator runs a `gcx fleet` read (e.g. list pipelines / list collectors)
  THEN the command succeeds and MUST NOT fail with `cloud token is required`.

- GIVEN the same OAuth-only context where the caller additionally has the Grafana
  Admin role
  WHEN the operator runs a `gcx fleet` mutation (create/update/delete pipeline or
  collector)
  THEN the request is proxied through the collector-app `fleet-management-api`
  path and completes successfully.

- GIVEN an OAuth-only context with the Viewer role and no Cloud token
  WHEN the operator runs a `gcx instrumentation` read (e.g. list clusters/services
  or get instrumentation status)
  THEN the command succeeds without requiring any Cloud access-policy token.

- GIVEN an OAuth-only context with the Grafana Admin role and no Cloud token
  WHEN the operator runs a `gcx instrumentation` setup/mutation
    (e.g. set app/K8S instrumentation, run K8s discovery/monitoring)
  THEN the backend datasource URLs are sourced via the collector-app
  instance-metadata proxy route, embedded in the request body, and the mutation
  completes successfully.

- GIVEN any fleet or instrumentation command
  WHEN it constructs its HTTP request
  THEN the request URL is
  `cfg.Host + /api/plugin-proxy/grafana-collector-app/fleet-management-api` + the
  unchanged Connect service/method path, and the request carries no client-set
  Basic auth, no client-set `Authorization` header, and no `X-Prom-*` /
  `X-Scope-OrgID` header.

- GIVEN a caller holding only the Viewer role
  WHEN they attempt a fleet or instrumentation mutation (a create/update/delete
  or setup call that requires the Grafana Admin role)
  THEN the command exits with an actionable error that identifies the missing
  Grafana Admin role and is distinguishable from an FM request-rejection error.

- GIVEN the amended `CONSTITUTION.md`
  WHEN the dependency rule and Project Identity intro are read
  THEN **Fleet** no longer appears in the `httputils.NewDefaultClient` "outside
  the Grafana server" list, and the document states fleet/instrumentation reach FM
  via the collector-app proxy at `cfg.Host` using `rest.HTTPClientFor()`.

- GIVEN the regenerated CLI reference
  WHEN `GCX_AGENT_MODE=false mise run reference` and `mise run reference-drift`
  run in CI
  THEN the `gcx fleet` / `gcx instrumentation` reference docs state the Viewer
  (read) / Grafana Admin (mutate) role requirement and the drift check passes.

- GIVEN the reconciled test suites
  WHEN `go test ./internal/fleet/... ./internal/providers/fleet/...
  ./internal/providers/instrumentation/...` runs
  THEN no test constructs the base client with `useBasicAuth`/instance-ID/token,
  auth assertions verify the plugin-proxy path/URL and bearer transport, and the
  Connect method paths and JSON wire format assertions are unchanged.

## Negative Constraints

- **NEVER** add a hybrid or fallback path that retains direct FM (Basic-auth)
  access — the cutover is clean and single-transport (FR-018).
- **NEVER** use `rest.HTTPClientFor()` for any API that remains outside `cfg.Host`;
  it injects the Grafana bearer token and would conflict with a product's own auth.
  It is correct here only because the traffic now targets `cfg.Host`.
- **DO NOT** set the FM Basic-auth credential, a client-side `Authorization`
  header, or the `X-Prom-Cluster-ID` / `X-Prom-Instance-ID` / `X-Scope-OrgID`
  headers from gcx — the round-tripper and the proxy inject these.
- **DO NOT** require a Cloud access-policy token (`cloud.token` /
  `GRAFANA_CLOUD_TOKEN`) or `AgentManagementInstanceURL`/`ID` for any fleet or
  instrumentation command.
- **DO NOT** change the typed method surfaces, Connect service/method path
  suffixes, or the JSON wire format of request/response bodies.
- **DO NOT** document `grafana-collector-app` plugin internals in the public gcx
  repo (no token-injection mechanics, no plugin.json route templates, no internal
  route enumeration) — describe only gcx's own proxy calls.

## Risks

| Risk | Impact | Mitigation |
|---|---|---|
| The instance-metadata proxy response shape may not match the fields `BackendURLsFromStack` consumes (e.g. `HMInstancePromURL`, `HMInstancePromID`, `HLInstanceURL`, `HTInstanceURL`, `HPInstanceURL`). | Instrumentation setup builds malformed or empty backend URLs; mutations fail or misconfigure ingestion. | Verify the proxied instance response carries the same fields during implementation; add a decode/validation step and cover with a test against the confirmed shape (Open Question OQ-1). |
| RBAC/role misconfiguration (caller lacks Viewer or Admin) surfaces as an opaque proxy status. | Users cannot tell a missing-role denial from an FM rejection; support burden rises. | Implement error mapping (FR-016) distinguishing insufficient-role from FM request-rejection, and document the Viewer/Admin requirement in command help (FR-014). |
| Breaking change: existing callers relying on direct FM access + an FM/Cloud token now authenticate to Grafana instead. | Scripts/CI carrying only an FM token break at cutover. | Announce in `CHANGELOG.md` and add a doc callout on the fleet/instrumentation commands stating the new Grafana-credential requirement and role gating. |
| The exact instance-metadata proxy route path is not yet pinned against the plugin. | Wrong path yields a 404 that looks like a transport bug. | Treat the path as build-time verification against the plugin (Open Question OQ-1); keep it out of committed public docs until confirmed. |
| Wrong proxy prefix (`/api/plugins/<id>/resources/...` instead of `/api/plugin-proxy/<id>/...`). | Silent 404 — collector-app has no backend, so the resources endpoint never resolves. | Follow ADR 001 explicitly; assert the app plugin-proxy prefix in the base-client test (FR-001, FR-017). |

## Open Questions

- **OQ-1** [NEEDS CLARIFICATION] — The exact route path and JSON response shape of
  the collector-app Grafana-authenticated instance-metadata proxy route (Viewer
  role) used to source instrumentation backend datasource URLs. Confirm the path
  and verify the response carries every field `BackendURLsFromStack` consumes,
  against the plugin at build time.
- **OQ-2** [RESOLVED] — Should a hybrid plugin-or-direct transport be retained?
  No; rejected in ADR 001 (commands are Cloud-only and the plugin is guaranteed on
  all Cloud stacks, so a fallback branch serves no real environment).
- **OQ-3** [RESOLVED] — Does `instrumentation` still need a Cloud token for backend
  URLs? No; the ADR 001 validation (2026-07-07) routes that lookup through the
  proxy, leaving no Cloud-token dependency.
- **OQ-4** [DEFERRED] — Fine-grained per-resource RBAC (beyond Viewer/Admin) is a
  future plugin-side enhancement, out of scope for this cutover.
- **OQ-5** [DEFERRED] — Revisit a non-GCOM way to set the FM URL (and thus a
  possible direct path) only if FM becomes reachable outside Grafana Cloud.
