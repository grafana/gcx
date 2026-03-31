# Decision Tree: Provider vs Resources Command

When should you create a new provider vs using the existing `gcx resources` command?

## Quick Decision

```
Does the product expose a K8s-compatible API via Grafana's /apis endpoint?
├── YES → Use existing `gcx resources` command (no provider needed)
│         The resource is already discoverable and manageable.
│
└── NO → Does the product have its own REST API?
    ├── YES → Create a new provider
    │         The provider wraps the product's REST API and translates
    │         to/from the K8s envelope format.
    │
    └── NO → The product likely has no external API.
              Check if it's a UI-only feature or if the API is internal.
              → Cannot integrate without an accessible API.
```

## Detailed Criteria

### Use `gcx resources` (NO provider needed) when:

- The product registers K8s-style CRDs accessible via `/apis/{group}/{version}/...`
- Resources appear in `gcx resources schemas`
- Standard CRUD operations work via the dynamic client
- No product-specific auth beyond the Grafana service account token

**Examples**: Dashboards, Folders, AlertRules, ContactPoints — these all use
Grafana's native K8s API and need no provider.

### Create a new provider when:

- The product uses a plugin API (`/api/plugins/{id}/resources/...`)
- The product requires product-specific authentication or configuration
- The product's API returns non-K8s response envelopes
- You need product-specific commands beyond basic CRUD (e.g., `status`, `timeline`)
- The product has multiple related resource types that should be grouped

**Examples**: SLO (plugin API, custom status commands), Synthetic Monitoring
(separate service URL + token), OnCall (separate API).

**Important: CRUD via unified resources path**. Once a provider implements
`ResourceAdapter` and registers a static descriptor (via `adapter.Register()`
in its `init()` function), its resource types become accessible through the
unified `gcx resources` command:

```
gcx resources get slo           # replaces: gcx slo definitions list
gcx resources get slo/<uuid>   # replaces: gcx slo definitions get <uuid>
gcx resources push slo -p ./   # replaces: gcx slo definitions push
gcx resources pull slo -p ./   # replaces: gcx slo definitions pull
gcx resources delete slo/<id>  # replaces: gcx slo definitions delete <id>
```

The provider-specific top-level commands (`gcx slo`, `gcx synth`,
`gcx alert`) remain available for backward compatibility but print a
deprecation warning to stderr. New providers should implement `ResourceAdapter`
alongside the existing command tree from the start.

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
# Test if K8s API works (if yes → no provider needed)
curl -s -H "Authorization: Bearer $TOKEN" \
  "$GRAFANA_URL/apis/" | jq '.groups[].name' | grep {product}

# Test plugin API (if this works → provider needed)
curl -s -H "Authorization: Bearer $TOKEN" \
  "$GRAFANA_URL/api/plugins/{product}-app/resources/v1/"
```

If neither works, investigate the product's source code for route registration.
