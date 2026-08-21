# Placement and Readiness

Contents:
1. [Surface necessity — is a new command needed at all?](#1-surface-necessity)
2. [Axis 1 — backend surface](#2-axis-1--backend-surface)
3. [Axis 2 — gcx wiring](#3-axis-2--gcx-wiring)
4. [Wiring mechanics](#4-wiring-mechanics)
5. [Backend-readiness gate](#5-backend-readiness-gate)
6. [The placement section](#6-the-placement-section)

## 1. Surface necessity

Every gcx leaf competes for an agent's attention during command selection. Before
proposing a new one, inventory what exists:

```bash
bin/gcx help-tree
bin/gcx commands --flat -o json
bin/gcx resources list-types
bin/gcx providers list
bin/gcx agent skills list
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
| Grafana K8s API (`/apis/...`) | `bin/gcx api /apis/` — look for the product's API group |
| Plugin / product REST API | `bin/gcx api /api/plugins/` or the product's own service URL with its docs |
| GCOM control plane (stack/org-level) | grafana.com API, cloud token — different auth domain from stack tokens |
| Datasource query API | The data lives behind a datasource plugin and is queried per-datasource UID |
| Client-only workflow | No new backend calls — composition of existing gcx commands |

## 3. Axis 2 — gcx wiring

gcx has **two architectural tiers** (K8s resource tier and Cloud provider tier);
everything below is a wiring option within or beside them, not a new tier:

- **No new code — for standard CRUD only.** If the type is externally accessible
  and discoverable on `/apis`, `gcx resources` already covers get/push/pull/delete
  through dynamic discovery. At most, add agent metadata for the type (see
  `internal/agent/known_resources.go`).

  **This settles CRUD, not the command surface.** Product-specific operations on a
  K8s-backed product still need their own placement analysis: `gcx dashboards` and
  `gcx alert` both exist as dedicated command trees over products that *are*
  on `/apis`, because "restore this version", "export this policy tree" and
  "validate this rule" are not CRUD verbs. Presence on `/apis` never by itself
  decides that no commands are warranted.

  One hard constraint if your answer is a commands-only provider calling the K8s
  dynamic client: `CONSTITUTION.md` § Architecture Invariants records
  `internal/providers/dashboards/` as **the one documented exception** (ADR 016).
  A second one extends that exception, so it needs explicit human approval and a
  CONSTITUTION change — say so in the placement section rather than assuming it.
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
  `internal/datasources/providers/<kind>.go`. Registration mounts the typed
  `datasources <kind>` subtree automatically; it does **not** reach the generic
  auto-detecting `datasources query`. That second decision is a judgement no test
  catches: add a `dispatch` entry if the generic `<uid> <expr>` form can honestly
  carry your query, or a `redirects` entry naming your typed command if it
  cannot — both tables live in `cmd/gcx/datasources/query_routes.go`. Reasoning,
  the worked CloudWatch example and the ordering requirement:
  [distribution-and-gates.md § The gap CI does not cover](distribution-and-gates.md#the-gap-ci-does-not-cover-and-it-is-a-judgement-call).
- **Skill-only** — a portable workflow for people using gcx across projects
  ships under `claude-plugin/skills/`. A workflow used only while contributing
  to this repository lives under `.claude/skills/`. Neither needs Go code.
- **`gcx api`** is a raw diagnostic fallback (token cost: large, exempt from the
  structured output contract). It is never the integration target — if the
  paved answer to a recurring need is "curl through `gcx api`", the need isn't
  integrated yet.

## 4. Wiring mechanics

| Wiring | Registration | Reference implementation | Governing docs |
|--------|--------------|--------------------------|----------------|
| K8s resource tier | none (dynamic discovery) | `internal/resources/` | ARCHITECTURE.md §1, docs/architecture/resource-model.md |
| Cloud provider | single `providers.Register()` in `init()` + blank import in `cmd/gcx/root/command.go` | `internal/providers/slo/provider.go` | docs/reference/provider-guide.md, docs/design/provider-checklist.md |
| Adapter-backed resource | returned from `Provider.TypedRegistrations()` — never call `adapter.Register()` directly | `internal/providers/irm/oncall_adapter.go` | docs/architecture/patterns.md §16-18, CONSTITUTION § Architecture Invariants |
| Signal command | `signals.Descriptor` + `signals.Command()` | `internal/providers/metrics/provider.go` | ARCHITECTURE.md §3 |
| Datasource kind | `datasources.RegisterProvider()` in `internal/datasources/providers/<kind>.go` (package already blank-imported). Generic routing is a **separate, conditional** decision — a `dispatch` entry in `cmd/gcx/datasources/query_routes.go` if `<uid> <expr>` fits, a `redirects` entry if it does not ([detail](distribution-and-gates.md#the-gap-ci-does-not-cover-and-it-is-a-judgement-call)) | `internal/datasources/providers/prometheus.go` | ADR 001, docs/architecture/patterns.md §12 |
| Portable user skill | directory under `claude-plugin/skills/` (auto-embedded) + row in `claude-plugin/README.md` | any sibling skill | AGENTS.md Key Conventions |
| Repository contributor skill | directory under `.claude/skills/` (discovered from the checkout; not embedded) | `.claude/skills/add-provider/` | AGENTS.md Key Conventions |

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

**Unknowns block by material risk, not by category.** Answer the questions above
from the product's docs, its source, or a probe you actually ran — not from what
the API plausibly does. Then weigh what is still unknown.

An unknown makes the outcome **backend prerequisite** (or bounded bootstrap,
where all three of its conditions hold) when it bears on any of:

- **API ownership or stability** — an unidentified owner, or an internal or
  experimental API, cannot be wrapped in a public contract. This one is easy to
  wave through as "just paperwork"; it is not. It decides whether the surface you
  build can be supported at all.
- **Authentication or RBAC** — including whether existing gcx auth reaches it.
- **Security or sensitive data** — anything needing redaction rules.
- **Mutation safety** — what a partial or retried write does.
- **Correctness of the result** — an unknown route or payload shape, or any
  semantics you would otherwise be guessing. You cannot write an honest client
  against a request you have not seen.
- **Bounded completeness** — limits and pagination behaviour that decide whether
  your output is partial, and whether you can disclose that honestly
  ([self-review.md](self-review.md) T3).

For those, there is no "ready, pending verification": that phrasing turns an open
question into a commitment the backend has not made, and the contract built on it
inherits the guess.

Every other unknown is recorded as `UNVERIFIED` with the exact probe a reviewer
should run, and the work proceeds. An unmeasured rate limit on a read-only
discovery command is a risk to disclose, not a reason to stop.

**A probe that did not run is not evidence.** If a probe is unavailable,
inconclusive, or outside the target you were placed in scope for, do not run it
and do not read its absence as a negative result — a product that is plan-gated,
disabled on the configured stack, or served in a different tenant looks identical
to a product that has no API at all. Record the exact probe a reviewer should run
as `UNVERIFIED` in the placement section, and carry it into the
`Unverified assumptions` line of the final summary.

Outcome (one of four, written down):

1. **Ready** — every **material-risk** question above resolved from evidence.
   Non-material unknowns may remain, recorded as `UNVERIFIED` with their probes;
   they do not downgrade the outcome. Proceed to the contract
   (contract-and-tests.md).
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

## 6. The placement section

Four bullets, shown in your plan and carried forward to any sibling skill you
hand off to. Not a document, and not something to wait for approval on — if the
repository, the governing docs and a probe settle it, it is settled.

```text
Capability: <one line>

- Necessity: <reuse|extend|consolidate|new leaf|not gcx>
    nearest sibling <command> — an agent picks between them by: <one sentence>
- Path: gcx <…> — derived from <naming-guide rule + tree precedent>
- Backend + wiring: <surface> verified by <probe / doc link>
    → <no-new-code|cloud|provider(plain|adapter|signal)|datasource|skill-only>
- Readiness: <ready|backend-prerequisite(owner)|bounded-bootstrap|not-gcx>
    boundary items: <auth/RBAC, limits, pagination, data reduction — or none>

UNVERIFIED: <probes not run or inconclusive, and what a reviewer must run — or none>
Unresolved: <what evidence could not settle — or none>
```

If `Unresolved` is non-empty, that is what you ask about (one grouped question,
with the evidence and a recommendation) — not a request to approve the section.
