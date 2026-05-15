# Research: Vulnerability Observability Provider

**Date**: 2026-05-15
**Status**: exploratory
**Confidence**: Medium — API mapped empirically; introspection disabled; no public schema docs
**Sources**: Direct API probes against `https://ops.grafana-ops.net` via `gcx api`

## Executive Summary

- The `grafana-vulnerabilityobs-app` plugin exposes vulnerability findings
  via a **plugin-proxied GraphQL endpoint**, not REST. It is read-only from
  the user's perspective; all mutations are driven by scanner ingestion on
  the server side.
- The endpoint accepts **unpersisted GraphQL documents** in addition to
  Apollo persisted-query hashes used by the UI. This means a gcx provider
  does not need to track operation hashes, but **introspection is
  disabled** so the schema must be discovered empirically.
- Three queries cover the v1 scope: `groups`, `sources` (projects), and
  `issues` (CVE findings). The same CVE may appear multiple times per
  version when reported by multiple scanners; consumers must deduplicate
  on `(package, target)` when summarizing.
- Auth model matches the existing KG (Asserts) provider: requests flow
  through Grafana's instance plugin proxy, so reusing the active Grafana
  session via `config.NamespacedRESTConfig` works as-is.

## Problem

A team lead asked whether gcx can surface Grafana Vulnerability
Observability data (CVEs, SLO posture, fix recommendations) for a repo or
group. Today the only way to get this data programmatically is to dig
operation names and persisted-query hashes out of the UI and call
`gcx api` directly. That works for one-off scripting but isn't a stable
contract — operation hashes rotate, and consumers must reimplement the
GraphQL wiring each time.

## Findings

### API surface

| Path | Notes |
|------|-------|
| `POST /api/plugin-proxy/grafana-vulnerabilityobs-app/api-proxy/graphql/query` | All queries. Single GraphQL endpoint. |
| `Content-Type: application/json` | Body: `{operationName, query, variables}`. |
| Persisted-query extension is **optional** | Plain documents work too — confirmed below. |
| `__schema` introspection | **Disabled.** Returns `{"data":{"__schema":null}, "errors":[{"message":"introspection disabled", ...}]}`. |

### Auth model

- Plugin proxy on the Grafana instance. The plugin forwards to the
  vulnerability-obs backend after stamping the user's Grafana auth.
- No separate token, no separate URL. Inherits the same auth as the active
  Grafana session.
- Matches the KG provider's model (`/api/plugins/grafana-asserts-app/...`)
  and the IRM-plugin variants — `rest.HTTPClientFor(&cfg.Config)` on a
  `config.NamespacedRESTConfig` is sufficient.

### Operations confirmed

The UI uses Apollo persisted queries; we re-mapped each to an unpersisted
GraphQL document and confirmed responses against ops.

#### `groups` — list all teams/groups

```graphql
query Groups {
  groups { id name }
}
```

No variables. Returns 50+ groups (e.g., `feO11y` = id 57, `o11y` = 16,
`RUM` = 69, `mobileO11y` = 68).

#### `sources` — list projects, paginated

```graphql
query Projects($filters: SourceFilters!, $first: Int, $after: Int) {
  sources(filters: $filters, first: $first, after: $after) {
    metadata { totalCount }
    response {
      id
      name              # e.g. "grafana/faro-web-sdk"
      type              # "repository"
      origin            # "github"
      visibility        # "public" | "private"
      integration { id name type }
      groups { id name }
      versions {
        id
        tag             # "main", "v2.6.3", ...
        publishDate
        lowestSloRemaining
        totalCveCounts { critical high medium low }
      }
    }
  }
}
```

`SourceFilters` (confirmed):

| Field | Type | Notes |
|---|---|---|
| `groupId` | `String!` (stringified int) | Filter to one group. |
| `name` | `String` | Substring match on `name`. |
| `sortBy` | enum | `CRITICALS_DESC`, `HIGHS_DESC`, etc. |
| `enabledOnly` | `Boolean` | Hide disabled sources. |
| `versionFilters` | `{ hideK8s, showArchived }` | Excludes k8s-scan versions, archived sources. |

Fields tried that are **not** on `SourceFilters` (errored at 422):
`first`, `after` (they're on `sources(...)`, not on filters);
`cveCountsScope` (sits on the persisted-query selector, not on filters);
`searchTerm`, `search`, `query`, `keyword` (use `name`).

#### `issues` — CVE findings for a version

```graphql
query Issues($filters: IssueFilters!) {
  issues(filters: $filters) {
    response {
      id
      package
      installedVersion
      fixedVersion
      target              # "yarn.lock", "go.mod", "/yarn.lock", ...
      sloRemaining        # days; negative = breached
      tool { name }       # "grype" | "trivy" | "dependabot" | "osv-scanner"
      cve { cve severity cvssScore title }
    }
  }
}
```

`IssueFilters` confirmed field: `versionId` (`String!`). Other fields
likely exist (severity, tool) but were not probed since client-side
filtering is trivial for the v1 scope.

### Resource relationships

```
Group ── many ─── Source (repo)
                    │
                    └── many ─── Version (tag, e.g. "main", "v2.6.3")
                                  │
                                  └── many ─── Issue (CVE finding, per-scanner)
```

A single CVE may appear N times per version (once per scanner that flagged
it). Consumers that summarize must deduplicate on `(package, target)`.

### Sample real call

```
$ gcx api '/api/plugin-proxy/grafana-vulnerabilityobs-app/api-proxy/graphql/query' \
    -X POST -H 'Content-Type: application/json' \
    -d '{"query":"query I($f: IssueFilters!) { issues(filters:$f) { response { package fixedVersion cve { cve severity } } } }","variables":{"f":{"versionId":"10355"}}}'
{"data":{"issues":{"response":[
  {"package":"axios","fixedVersion":"1.15.2","cve":{"cve":"CVE-2026-42044","severity":"CRITICAL"}},
  ...
]}}}
```

feO11y group snapshot at the time of this research:

| Repo | versionId (main) | Crit | High | Med | Low | Worst SLO |
|---|---:|---:|---:|---:|---:|---:|
| grafana/faro-javascript-bundler-plugins | 10355 | 3 | 6 | 6 | 1 | -2 |
| grafana/faro-web-sdk | 10354 | 3 | 4 | 5 | 1 | 21 |
| grafana/app-o11y-kwl-endpoint | 6525 | 0 | 12 | 4 | 0 | -41 |
| grafana/app-o11y-kwl | 10350 | 0 | 3 | 8 | 1 | 8 |

## Recommendations

- Build a **read-only** provider, modeled on `internal/providers/kg/`.
  - `gcx vulnobs groups list`
  - `gcx vulnobs projects list [--group <name|id>]`
  - `gcx vulnobs issues list --version <id> | --repo <name> [--tag <tag>]`
- Reuse `config.NamespacedRESTConfig` for auth; no new config keys needed.
- Skip the `TypedRegistrations()` / k8s envelope mapping for v1 — the API
  has no mutations, so there's nothing to push/pull as a Resource. List
  output uses the standard output codecs (`-o json/yaml/text`).
- Keep the fix/remediation workflow **out** of the provider. That logic
  reaches the local filesystem and `gh`, neither of which belong inside a
  CRUD-over-remote-API provider. It stays in the
  `grafanacloud-vuln-obs` Agent Skill, which will be retargeted to call
  `gcx vulnobs ...` instead of `gcx api`.

## Open questions

- **`versionId` resolution.** UI uses `versionId` directly; agents will
  more naturally pass `--repo grafana/faro-web-sdk [--tag main]`. The
  provider will need to do a `sources` lookup to resolve repo+tag →
  versionId. Cache TTL? For v1, no cache — issue is a one-call surface.
- **Pagination on `issues`.** We did not see pagination args on `issues`
  in the persisted queries we captured; first-call results have always
  returned the full list. Confirm during implementation.
- **Severity / tool filters on `issues`.** Likely exist server-side. For
  v1 we'll filter client-side; revisit if response sizes grow.

## Sources

1. `https://ops.grafana-ops.net` Vulnerability Observability UI — captured
   request URLs for `getGroupsFilter`, `getProjectsList`,
   `getProjectsSummary`, `getVersionVulnerabilities` operations.
2. Direct API probes via `gcx api` (see "Sample real call" above).
3. KG provider implementation at `internal/providers/kg/` — reference
   for plugin-proxy auth pattern and command surface shape.
