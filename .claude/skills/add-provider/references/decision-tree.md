# Decision Tree: Provider vs Resources Command

> **Scope: direct invocation of `add-provider`.** If you arrived from
> `integrate-with-gcx`, its placement section already settled the tier with
> probe evidence — record that verdict and skip this file. Re-deriving it here
> duplicates work and risks contradicting a decision that was made against the
> live API.

When should you create a new provider vs using the existing `gcx resources` command?

## Quick Decision

```
Do you need only standard CRUD on a type that is externally accessible
and discoverable on Grafana's /apis endpoint?
├── YES → `gcx resources` already covers it; no provider needed for CRUD.
│         But if the product also has non-CRUD operations (restore a
│         version, export a tree, validate a rule), those still need
│         placement analysis — `gcx dashboards` and `gcx alert` are
│         dedicated command trees over /apis-backed products for exactly
│         that reason. Being on /apis does not settle the command surface.
│
└── NO → Does the product have its own REST API?
    ├── YES → Create a new provider
    │         The provider wraps the product's REST API. Plain provider
    │         commands are a complete integration on their own; resource
    │         types that belong in the `gcx resources` pipeline additionally
    │         get an adapter + K8s envelope mapping.
    │
    └── NO → The product likely has no external API.
              Check if it's a UI-only feature or if the API is internal.
              → Cannot integrate without an accessible API.

NOTE: if the design that falls out is a commands-only provider calling the
K8s dynamic client, CONSTITUTION.md § Architecture Invariants records
internal/providers/dashboards/ as the ONE documented exception (ADR 016).
A second extends that exception: explicit human approval and a
CONSTITUTION change are required before building it.
```

## Detailed Criteria

### Use `gcx resources` (NO provider needed) when:

- The product registers K8s-style CRDs accessible via `/apis/{group}/{version}/...`
- Resources appear in `gcx resources list-types`
- Standard CRUD operations work via the dynamic client
- No product-specific auth beyond the Grafana service account token

**Examples**: Dashboards, Folders, AlertRules, ContactPoints all use Grafana's
native K8s API and need no provider **for standard CRUD** — `gcx resources` covers
get/push/pull/delete on them today.

Note what that does not mean: three of those four also have provider command
trees, because their non-CRUD operations are not reachable through `resources`
(`gcx dashboards` for versions and snapshots, `gcx alert contact-points` /
`alert rules` for the alerting families). Only Folders has no provider. So this
row rules out a provider for CRUD, not for the product.

### Create a new provider when:

- The product uses a plugin API (`/api/plugins/{id}/resources/...`)
- The product requires product-specific authentication or configuration
- The product's API returns non-K8s response envelopes
- You need product-specific commands beyond basic CRUD (e.g., `status`, `timeline`)
- The product has multiple related resource types that should be grouped

**Examples**: SLO (plugin API, custom status commands), Synthetic Monitoring
(separate service URL + token), OnCall (separate API).

**Adapters are conditional, not mandatory.** Plain provider commands are valid
and first-class on their own — an adapter must never be created merely to
unlock a CRUD verb (CONSTITUTION § Provider Architecture). When a resource type
does belong in the unified pipeline, the provider implements `ResourceIdentity`
on the domain type, builds a `TypedCRUD[T]`-backed `adapter.Registration`
(non-nil `Schema`), and returns it from `TypedRegistrations()` — the single
`providers.Register()` call in `init()` performs the registration (provider code
never calls `adapter.Register()` directly). The type then ALSO becomes
accessible through `gcx resources`:

```
gcx resources get slos          # alongside: gcx slo definitions list
gcx resources get slos/<uuid>   # alongside: gcx slo definitions get <uuid>
gcx resources push -p ./        # alongside: gcx slo definitions push
gcx resources pull slos -p ./   # alongside: gcx slo definitions pull
gcx resources delete slos/<id>  # alongside: gcx slo definitions delete <id>
```

**Dual CRUD access paths are permanent** for adapter-backed resources
(CONSTITUTION § Provider Architecture): neither the provider commands nor the
`resources` path is deprecated, no deprecation warnings are printed, and both
must return identical JSON/YAML by construction (both go through the adapter's
TypedCRUD).

### Edge cases

| Situation | Decision | Reason |
|-----------|----------|--------|
| Product has K8s CRDs but they're internal-only | Create provider | CRDs not accessible externally |
| Product uses Grafana token but has custom API | Create provider | Non-K8s API needs adapter layer |
| Product has one simple endpoint | Consider provider | Even simple products benefit from typed config |
| Product is in beta with unstable API | Create provider, mark `v1alpha1` | Isolate instability in provider code |

## Auth Decision Matrix

| Auth Model | ConfigKeys | Implementation |
|------------|-----------|----------------|
| Same Grafana SA token, same server | Empty `[]` | Read `curCtx.Grafana.Token` directly |
| Same token, different base path | Empty `[]` | Construct URL from `curCtx.Grafana.Server` + product path |
| Separate product token | `[{Name: "token", Secret: true}]` | Read from provider config |
| Separate service URL + token | `[{Name: "url"}, {Name: "token", Secret: true}]` | Full separate client |

## Validation

Before committing to a provider, verify with a real API call:

```bash
# Test if the K8s API serves it (if yes → no provider needed for CRUD;
#  non-CRUD operations still need placement analysis)
curl -s -H "Authorization: Bearer $TOKEN" \
  "$GRAFANA_URL/apis/" | jq '.groups[].name' | grep {product}

# Test plugin API (if this works → provider needed)
curl -s -H "Authorization: Bearer $TOKEN" \
  "$GRAFANA_URL/api/plugins/{product}-app/resources/v1/"
```

If neither works, investigate the product's source code for route registration.
