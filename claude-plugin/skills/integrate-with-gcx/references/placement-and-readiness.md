# Placement and Readiness

Contents:
1. [Surface necessity — is a new command needed at all?](#1-surface-necessity)
2. [Axis 1 — backend surface](#2-axis-1--backend-surface)
3. [Axis 2 — gcx wiring](#3-axis-2--gcx-wiring)
4. [Wiring mechanics](#4-wiring-mechanics)
5. [Backend-readiness gate](#5-backend-readiness-gate)
6. [Placement memo template](#6-placement-memo-template)

## 1. Surface necessity

Every gcx leaf competes for an agent's attention during command selection. Before
proposing a new one, inventory what exists:

```bash
gcx help-tree
gcx commands --flat -o json
gcx resources list-types
gcx providers list
gcx agent skills list
```

Then decide, in writing, one of five outcomes:

| Outcome | When |
|---------|------|
| **Reuse** | An existing command already answers the question (possibly with a flag you didn't know about — read its `--help`) |
| **Extend** | An existing leaf covers 80% and a new flag/mode covers the rest without changing its contract |
| **Consolidate** | Your capability and an existing sibling overlap enough that an agent could not reliably pick between them — merge rather than add |
| **New independent leaf** | The capability has its own unambiguous activation scenario |
| **No new surface** | The need is met by `gcx api` for one-off diagnostics, a bundled skill, or nothing in gcx at all |

For a new leaf, name the **nearest existing sibling** and write one sentence an
agent could use to choose between them. If a person familiar with the tree cannot
say definitively which command applies to a given request, an agent cannot either.

## 2. Axis 1 — backend surface

Identify what actually serves the data. Verify empirically, don't assume:

| Surface | Probe |
|---------|-------|
| Grafana K8s API (`/apis/...`) | `gcx api /apis/` — look for the product's API group |
| Plugin / product REST API | `gcx api /api/plugins/` or the product's own service URL with its docs |
| GCOM control plane (stack/org-level) | grafana.com API, cloud token — different auth domain from stack tokens |
| Datasource query API | The data lives behind a datasource plugin and is queried per-datasource UID |
| Client-only workflow | No new backend calls — composition of existing gcx commands |

## 3. Axis 2 — gcx wiring

gcx has **two architectural tiers** (K8s resource tier and Cloud provider tier);
everything below is a wiring option within or beside them, not a new tier:

- **No new code** — a native K8s type on `/apis` is already discoverable and
  manageable via `gcx resources` (dynamic discovery). At most, add agent metadata
  for the type (see `internal/agent/known_resources.go`).
- **Cloud-tier command** — GCOM control-plane operations mount under `gcx cloud`.
- **Provider commands** — a product with its own REST API gets a provider
  (`internal/providers/<name>/`, 6-method interface). Within a provider, three
  implementation patterns, chosen per resource:
  - *Plain commands* — valid and first-class on their own. Commands-only
    providers return `nil` from `TypedRegistrations()`.
  - *Adapter-backed resources* — types that genuinely belong in the
    `gcx resources` push/pull pipeline get `TypedCRUD[T]` + `ResourceIdentity`
    + a registration with non-nil `Schema`. Never create an adapter merely to
    unlock a CRUD verb (CONSTITUTION § Provider Architecture).
  - *Cross-signal commands* — query/labels/series/metadata verbs shared across
    signals use `signals.Descriptor` (see `internal/signals/`).
- **Datasource provider** — a new queryable datasource kind implements
  `DatasourceProvider` (`internal/datasources/provider.go`) and self-registers in
  `internal/datasources/providers/<kind>.go`.
- **Skill-only** — a workflow over existing commands ships as a bundled Agent
  Skill under `claude-plugin/skills/`, no Go code.
- **`gcx api`** is a raw diagnostic fallback (token cost: large, exempt from the
  structured output contract). It is never the integration target — if the
  paved answer to a recurring need is "curl through `gcx api`", the need isn't
  integrated yet.

## 4. Wiring mechanics

| Wiring | Registration | Reference implementation | Governing docs |
|--------|--------------|--------------------------|----------------|
| K8s resource tier | none (dynamic discovery) | `internal/resources/` | ARCHITECTURE.md §1, docs/architecture/resource-model.md |
| Cloud provider | single `providers.Register()` in `init()` + blank import in `cmd/gcx/root/command.go` | `internal/providers/slo/provider.go` | docs/reference/provider-guide.md, docs/design/provider-checklist.md |
| Adapter-backed resource | returned from `Provider.TypedRegistrations()` — never call `adapter.Register()` directly | `internal/providers/irm/oncall_adapter.go` | docs/architecture/patterns.md §16-18, CONSTITUTION § Provider Architecture |
| Signal command | `signals.Descriptor` + `signals.Command()` | `internal/providers/metrics/provider.go` | ARCHITECTURE.md §3 |
| Datasource kind | `datasources.RegisterProvider()` in `internal/datasources/providers/<kind>.go` (package already blank-imported) | `internal/datasources/providers/prometheus.go` | ADR 001, docs/architecture/patterns.md §12 |
| Bundled skill | directory under `claude-plugin/skills/` (auto-embedded) + row in `claude-plugin/README.md` | any sibling skill | AGENTS.md Key Conventions |

## 5. Backend-readiness gate

gcx wraps product APIs; it does not fix them. Product teams own their backend
API shape, domain semantics, authentication and RBAC, scalability, and
domain-specific data reduction. Before designing commands, answer:

- **Owner** — who owns and approves use of this API? Are they aware?
- **Stability** — is the API public/stable, or internal/experimental?
- **Auth/RBAC** — does the stack token (or an existing gcx auth mechanism)
  cover it, or does it need credentials gcx doesn't manage?
- **Sensitive data** — does it return data that needs redaction rules?
- **Limits** — rate limits, tenant limits, response-size behavior?
- **Pagination and failure semantics** — documented? Does the API signal
  "more pages exist"? What do partial failures look like?
- **Data reduction** — would gcx have to page unbounded data client-side to
  compute something the backend should compute? That work belongs server-side.

Outcome (one of four, written down):

1. **Ready** — proceed to the contract worksheet.
2. **Backend prerequisite** — name the owner and the missing piece; gcx work
   waits or ships read-only around the gap.
3. **Bounded bootstrap** — proceed with an explicitly experimental, narrow
   surface without inventing a public contract the backend doesn't honor.
   For capabilities that reduce/aggregate bulk data client-side, bounded
   bootstrap additionally requires ALL of: a hard input ceiling in the
   contract; a written statement of why the server-side alternative cannot
   serve the need today (verified, not assumed); and a named product owner
   for the server-side successor. Without all three, the outcome is
   backend-prerequisite or not-gcx, not bootstrap.
4. **Not gcx** — the capability is product-owned (e.g., client-side bulk data
   mining); write a short boundary memo instead of code.

## 6. Placement memo template

```text
Capability: <one line>
Surface necessity: <reuse|extend|consolidate|new leaf|no new surface>
  Nearest sibling: <command> — disambiguation: <one sentence>
Backend surface: <k8s-apis|plugin-rest|product-rest|gcom|datasource-query|client-only>
  Verified by: <probe command / doc link>
gcx wiring: <no-new-code|cloud|provider(plain|adapter|signal)|datasource|skill-only>
Readiness: <ready|backend-prerequisite(owner)|bounded-bootstrap|not-gcx>
  Open boundary items: <auth/RBAC, limits, pagination, data reduction — or none>
Sign-off: <human reviewer who approved this memo>
```
