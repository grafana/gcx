# Vulnerability Observability Provider: Command Shape & Typed Resource Strategy

**Created**: 2026-05-15
**Status**: proposed
**Supersedes**: none

## Context

CONSTITUTION.md requires providers to register typed resource adapters via
`TypedRegistrations()` so that domain data flows through the unified
`gcx resources` pipeline (line 28–32). The single documented exception is
the `dashboards` provider, which uses the K8s dynamic tier instead
(`docs/adrs/dashboards-provider/001-...`). All other providers — including
read-only ones like `alert` — typed-register at least one resource.

CONSTITUTION line 42–47 explicitly accommodates read-only typed resources:

> The `Example` field MAY be nil for read-only resources (those without
> Create/Update support) since examples serve as templates for writable
> operations.

The KG provider has a `newListOnlyFactory` helper for exactly this case
(`internal/providers/kg/resource_adapter.go:227`) — `ListFn` is set,
`CreateFn`/`UpdateFn` are nil, and TypedCRUD falls back to list + name
filtering for `Get`.

Vulnerability Observability exposes two natural candidate types:

| Type | Listable without a parent? | Identity field | Notes |
|------|----------------------------|----------------|-------|
| `Source` (repo) | **Yes** | `name` (e.g. `grafana/faro-web-sdk`) | Top-level resource. Sources have stable names across scans. |
| `Issue` (CVE finding) | **No** — `IssueFilters.versionId` is required; calls without it return HTTP 502 from the upstream. | `id` (large int; appears persistent across same-day calls; cross-scan stability not empirically tested) | One row per scanner-run per finding, so the same logical CVE in a repo's `main` branch produces N rows (one per scanner: grype, trivy, dependabot, osv-scanner) — and **another N rows** for each tagged version of the same repo. |

CONSTITUTION line 130–135 governs the `Issue` case directly:

> **Sub-resources nest under their parent command.** If a resource cannot
> be listed or addressed without a parent ID (e.g. alerts require an
> alert group), it is a sub-resource. Sub-resources must not be
> registered as standalone typed adapters (no `ListFn` that ignores the
> parent).

`Issue` matches this definition: it cannot be listed without a parent
`versionId`. The CONSTITUTION rule is the load-bearing reason to leave
`Issue` outside `TypedRegistrations()`; the identity-multiplication
concern (one logical CVE × N scanners × N versions) reinforces it but is
not the primary justification.

## Decision

**Hybrid approach:**

1. **Register `Source` as a read-only typed resource.**
   - GVK: `vulnobs.grafana.app/v1alpha1` `Source`, plural `sources`,
     singular `source`.
   - Identity: `Source.Name` (e.g. `grafana/faro-web-sdk`).
   - Adapter uses KG's `newListOnlyFactory` pattern: `ListFn` returns
     all sources for the active context (with the same filters
     `gcx vulnobs projects list` accepts), `GetFn` is nil (TypedCRUD
     falls back to list+filter for name lookup).
   - `Schema` field is the standard envelope with a `spec` capturing
     `groups`, `integration`, `origin`, `visibility`, `versions[]` and
     `cveCounts`. `Example` is nil per CONSTITUTION line 45.
   - Available through both `gcx vulnobs projects list` (table-friendly,
     domain-shaped columns) and `gcx resources get sources.vulnobs.grafana.app`
     (k8s envelope, unified pipeline).

2. **Expose `Issue` as a sub-resource verb under `vulnobs projects`.** Per
   CONSTITUTION line 130–135, sub-resources nest under their parent and
   use the `$PARENT $VERB-$CHILD $PARENT_ID` shape:

   ```
   gcx vulnobs projects list-issues <versionId>
   gcx vulnobs projects list-issues --repo <owner/name> [--tag <tag>]
   ```

   `--repo`/`--tag` is a UX convenience that resolves to a `versionId`
   internally via one extra `sources` query. The positional `<versionId>`
   form satisfies the strict CONSTITUTION grammar; the flag form is
   syntactic sugar for agents who think in repo names.

3. **Per CONSTITUTION line 122–128**: because `Issue` is not adapter-
   registered, the sub-resource command must not mimic adapter verbs.
   `list-issues` is permitted; no `create-issue` / `update-issue` /
   `delete-issue` / `get-issue` aliases.

The full command surface becomes:

```
gcx vulnobs groups list                                  # provider-only (groups have no spec — just id+name)
gcx vulnobs projects list [--group <name|id>] ...        # mirrors typed-resource list with friendly columns
gcx vulnobs projects list-issues <versionId>             # sub-resource of Version (under Source)
gcx vulnobs projects list-issues --repo <name> [--tag t] # convenience alias for above

# Via the typed-resource tier:
gcx resources list sources.vulnobs.grafana.app
gcx resources get  source.vulnobs.grafana.app/grafana--faro-web-sdk
gcx api-resources | grep vulnobs
gcx explain source.vulnobs                               # schema lookup
```

Explicitly rejected:

- **No typed registrations at all** (the path proposed in the first draft
  of this ADR). The hybrid is closer to the spirit of CONSTITUTION line
  28–32, and `Source` cleanly fits the read-only typed-resource pattern
  KG already uses.
- **Typed `Issue` resource (with any identity scheme).** Sub-resources
  must not be registered as standalone typed adapters per CONSTITUTION
  line 130–135 — the rule is parent-ID-required, and `IssueFilters.versionId`
  is required by the upstream. Reviewable if the upstream ever adds a
  parentless `issues` query.
- **Standalone top-level `vulnobs issues list` command.** Sub-resources
  must nest under the parent command per the same CONSTITUTION rule.
  `vulnobs projects list-issues <versionId>` is the prescribed shape.
- **`vulnobs plan` / `vulnobs apply` in the provider.** These reach the
  local filesystem and `gh`. Providers are CRUD over a remote API. The
  fix workflow lives in the `grafanacloud-vuln-obs` Agent Skill.

## Consequences

- One new GVK in `gcx api-resources`. Agents discover `Source` through
  the unified discovery path; humans get readable tables through
  `gcx vulnobs projects list`. Both paths read from the same client.
- ~150 LOC added beyond the original spec (one extra file:
  `resource_adapter.go`, plus a `_test.go` covering identity round-trip
  and adapter wiring) — total stays well under 1000 LOC.
- `Groups` stay outside the typed-resource tier as well. They're an
  enum-shaped tag namespace, not a resource people would `pull` or
  `diff`. If demand emerges, register later.
- If the upstream API ever exposes a parentless `issues` query (Issues
  listable without a `versionId`) or surfaces a deterministic
  cross-version finding identifier, revisit this ADR to add an `Issue`
  typed registration.
