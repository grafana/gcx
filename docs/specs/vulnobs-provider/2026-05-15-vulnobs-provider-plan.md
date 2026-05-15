---
type: feature-plan
title: "Vulnerability Observability Provider"
status: draft
research: docs/research/2026-05-15-vulnobs-provider.md
created: 2026-05-15
---

# Vulnerability Observability Provider — Implementation Plan

Read-only gcx provider for Grafana Vulnerability Observability. Surfaces
groups, projects (sources), and CVE findings (issues) from the
`grafana-vulnerabilityobs-app` plugin via the Grafana instance plugin
proxy.

Single-stage scope. The provider registers `Source` as a read-only typed
resource (no Create/Update/Delete); `Issue` stays as a sub-resource under
`projects` per CONSTITUTION line 130–135. No push/pull/edit, no new config
keys — so it can ship as one PR without phasing.

## Pipeline Architecture

```
cmd/gcx/root (Cobra root)
  └─ providers.Register() init()
       └─ vulnobs.VulnobsProvider (NEW)
            ├─ Commands(): "vulnobs groups list" | "vulnobs projects list" | "vulnobs projects list-issues"
            ├─ TypedRegistrations(): Source (read-only) → sources.vulnobs.grafana.app
            └─ Client (NEW)
                 → POST /api/plugin-proxy/grafana-vulnerabilityobs-app/api-proxy/graphql/query
                    (auth: rest.HTTPClientFor(cfg.Config) — same Grafana token)
```

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Hand-rolled GraphQL POSTer, not codegen | [ADR-002](../../adrs/vulnobs-provider/002-graphql-client.md) — schema undocumented, three small queries, drift caught by unit tests against captured payloads. |
| Reuse Grafana token via `NamespacedRESTConfig` | [ADR-001](../../adrs/vulnobs-provider/001-auth-strategy.md) — only access path is the plugin proxy on the Grafana instance. |
| `Source` typed-registered read-only; `Issue` provider-only | [ADR-003](../../adrs/vulnobs-provider/003-command-shape-and-no-typed-resources.md) — `Source.name` is stable; `Issue` has no stable identity. Uses KG's `newListOnlyFactory` pattern. |
| Repo+tag shorthand for `issues list` | UX: agents naturally pass `--repo grafana/faro-web-sdk --tag main`; the provider resolves to `versionId` internally with one extra `sources` lookup. |
| Client-side `--severity` filter | Trivial response sizes; saves probing for the right server-side filter field. Revisit if data volumes grow. |

## File Tree

```
internal/providers/vulnobs/
  provider.go               # VulnobsProvider type + init() + Provider iface + TypedRegistrations(Source)
  client.go                 # HTTP client + GraphQL POSTer + repo/version resolution
  client_test.go            # httptest-backed unit tests
  commands.go               # cobra commands: groups, projects, issues
  commands_test.go          # command wiring tests
  types.go                  # GraphQL response structs + ResourceIdentity on Source
  types_identity_test.go    # GetResourceName / SetResourceName round-trip
  resource_adapter.go       # Source adapter via newListOnlyFactory pattern + Schema
  resource_adapter_test.go  # adapter wiring + descriptor + schema-non-nil

cmd/gcx/root/command.go
  # add: _ "github.com/grafana/gcx/internal/providers/vulnobs"

docs/research/2026-05-15-vulnobs-provider.md   # (already written)
docs/adrs/vulnobs-provider/{001,002,003}*.md   # (already written)
docs/specs/vulnobs-provider/2026-05-15-*.md    # (this file)
```

Estimated size: ~700-800 LOC including tests.

## Command Surface

```
gcx vulnobs                              Vulnerability Observability data
gcx vulnobs groups list                  List all groups (id + name)              # provider-only
gcx vulnobs projects list                List projects with CVE counts            # mirrors typed-resource list with friendly columns
   --group <name|id>                     Filter to one group (looks up name -> id)
   --sort CRITICALS_DESC|HIGHS_DESC|SLO_ASC
   --first N (default 30)
   --show-archived
   --include-k8s
gcx vulnobs projects list-issues <versionId>   List CVE findings for a Version    # sub-resource per CONSTITUTION line 130-135
   --repo <owner/name>                   OR resolve from repo+tag (replaces positional <versionId>)
   --tag <tag> (default "main")          Tag to resolve when --repo is used
   --severity CRITICAL,HIGH              Comma-separated severity filter (client-side)

# Via the typed-resource tier (powered by the same client):
gcx resources list sources.vulnobs.grafana.app
gcx resources get  source.vulnobs.grafana.app/grafana--faro-web-sdk
gcx api-resources | grep vulnobs
gcx explain source.vulnobs               # schema lookup
```

All commands honor `-o json|yaml|text|wide` via the standard codec
registry. `list-issues` accepts either a positional `<versionId>` (the
canonical sub-resource form) or `--repo <name> [--tag <tag>]` (resolves
to versionId via one extra `sources` query).

## Compatibility

- **Continues working unchanged**: every existing command.
- **Deprecated**: nothing.
- **Newly available**: `gcx vulnobs ...` subtree (3 commands).
- **Skill update (separate)**: `grafanacloud-skills/skills/grafanacloud-vuln-obs/scripts/vulnobs` will be retargeted in a follow-up to call `gcx vulnobs` instead of `gcx api`. That change is out of scope for this PR; the provider can ship first.

## Verification (Smoke Test Plan)

Executed in Stage 4 against `https://ops.grafana-ops.net` with an
authenticated `ops` context.

```bash
# Build
GCX_AGENT_MODE=false mise run all

# Provider registered
gcx providers | grep vulnobs

# Config view does not produce vulnobs keys (no keys to redact)
gcx config view | grep -i vulnobs || echo "ok: no vulnobs config keys"

# Help tree includes vulnobs
gcx help-tree | grep vulnobs

# Groups
gcx vulnobs groups list | head -5
gcx vulnobs groups list -o json | jq '.[0]'

# Projects
gcx vulnobs projects list --group feO11y
gcx vulnobs projects list --group feO11y -o json | jq '.[0].name'
gcx vulnobs projects list --group 57 -o json | jq 'length'   # numeric group ID

# Issues (canonical sub-resource form: positional versionId)
gcx vulnobs projects list-issues 10355 -o json | jq '.[0].cve.cve'

# Issues (repo shorthand)
gcx vulnobs projects list-issues --repo grafana/faro-web-sdk --tag main \
  --severity CRITICAL,HIGH | head -10
gcx vulnobs projects list-issues --repo grafana/faro-web-sdk -o json \
  | jq 'map(select(.cve.severity == "CRITICAL")) | length'

# Typed-resource tier
gcx api-resources | grep vulnobs                                     # sources.vulnobs.grafana.app listed
gcx resources list sources.vulnobs.grafana.app                       # same data as `vulnobs projects list`
gcx resources get  source.vulnobs.grafana.app/grafana--faro-web-sdk  # one source as k8s envelope
gcx explain source.vulnobs.grafana.app                               # JSON Schema (Example MAY be nil per CONSTITUTION line 45)

# Bad inputs
gcx vulnobs projects list-issues                              # no positional or --repo -> validation error
gcx vulnobs projects list-issues doesnotexist                 # graphql 502 surfaces as actionable error
gcx vulnobs projects list-issues 10355 --repo grafana/x       # mutual exclusion violation -> validation error
gcx vulnobs projects list --group doesnotexist                # actionable "unknown group" error
```

Pass criteria: every command exits 0 on the happy path; the three
"bad inputs" cases produce single-line, actionable error messages and
non-zero exit codes per `DESIGN.md`.

## Test Plan

### Unit (`go test ./internal/providers/vulnobs/...`)

- `client_test.go`:
  - `do()` posts JSON, sets `Content-Type: application/json`, surfaces
    GraphQL `errors[]` as Go errors.
  - `Groups()`, `Projects()`, `Issues()` decode captured fixtures
    (one per query, copied from real `gcx api` output).
  - `ResolveVersion("grafana/faro-web-sdk", "main")` does the
    `Projects` call and returns the right `versionId`.
- `commands_test.go`:
  - `vulnobs projects list-issues` validation: requires positional XOR `--repo`; mutually exclusive.
  - `vulnobs projects list --group` accepts numeric and named groups.
- `types_identity_test.go`:
  - `Source.GetResourceName()` round-trips through `SetResourceName(name)`
    (CONSTITUTION line 33–36 requirement).
- `resource_adapter_test.go`:
  - Registration descriptor matches GVK `vulnobs.grafana.app/v1alpha1 Source`.
  - `Schema` field is non-nil and parses as JSON Schema.
  - Adapter `List()` returns the same data as the client's `Projects()`.
  - Create/Update/Delete return a "not supported" error.

### Integration / Smoke

Manual against ops (see Verification section above).

## Acceptance Criteria

- GIVEN an authenticated `ops` context
  WHEN running `gcx vulnobs projects list --group feO11y`
  THEN four repos are listed with CVE counts matching the UI.
- GIVEN any context
  WHEN running `gcx vulnobs projects list-issues` with neither a positional `<versionId>` nor `--repo`
  THEN gcx exits 2 with a single-line error suggesting either input.
- GIVEN a `vulnobs` provider config (none required)
  WHEN running `gcx config view`
  THEN no vulnobs keys appear (nothing to redact).
- GIVEN `mise run all`
  THEN it passes with the new provider added.

## Negative Constraints

- MUST NOT add new config keys to the provider.
- MUST NOT introduce a GraphQL client library dependency.
- MUST register `Source` as a read-only typed resource (no Create/Update/Delete).
- MUST NOT register `Issue` as a typed resource (no stable identity).
- MUST NOT expose mutations on provider commands (no `create`, `update`, `delete` verbs).
- MUST NOT shell out to `gh` or touch the local filesystem from the
  provider; the fix workflow stays in the Agent Skill.
