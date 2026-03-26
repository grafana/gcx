# Provider Discovery Guide

> How to research and design a new provider *before* you start implementing it.
> Companion to [provider-guide.md](provider-guide.md) (which covers implementation).

## When to Use This Guide

Use this guide when you're adding a provider for a Grafana product you haven't
integrated before. The goal is to answer the design questions that
[provider-guide.md](provider-guide.md) assumes you already know — API shape,
auth model, resource structure, command surface — before writing any code.

---

## 1. Discovery Process

Research the product in this order. Each step builds on the previous one.

### 1.1 Map the API Surface

Start with the product's OpenAPI spec (if one exists) to get the endpoint
inventory, request/response schemas, and auth requirements.

Where to find it:
- Check the product's GitHub repo for `openapi.yaml` or `swagger.json`
- Look under `/api/plugins/{plugin-id}/resources/` in Grafana's API
- Search for `{product}-openapi-client` repos under `github.com/grafana/`

What to capture:
- Base path (e.g. `/api/plugins/grafana-slo-app/resources/v1/slo`)
- Auth scheme (Bearer token, API key, OAuth)
- Endpoints: method, path, purpose
- Response wrappers (each product may use different envelope shapes)
- Async behavior (does POST return 201 Created or 202 Accepted?)
- Pagination (offset-based, cursor-based, or none?)

**Warning**: OpenAPI specs are often incomplete. Treat them as a starting point,
not the truth. See step 1.3.

### 1.2 Check Existing Tooling

If a Terraform provider exists for this product, it's a goldmine for
understanding the resource schema and CRUD behavior:

```
github.com/grafana/terraform-provider-grafana → internal/resources/{product}/
```

What to extract:
- Schema fields, types, and validation rules
- Required vs optional fields
- Create/read/update/delete patterns and edge cases
- Any special handling (e.g. custom UUIDs, async writes, idempotency checks)

Also check for:
- Official Go SDK or client library
- CLI tools the product already ships
- K8s operators or CRDs

### 1.3 Inspect Source Code

Products often have undocumented endpoints, query types, or behavior not
reflected in their OpenAPI spec. Check the product's source repo:

```
github.com/grafana/{product} → pkg/api/   (API handlers)
                              → pkg/plugin/ (route registration)
                              → definitions/ (CRD schemas, if any)
```

What to look for:
- Endpoints registered in route handlers but missing from the OpenAPI spec
- Enum values not listed in the schema (e.g. additional query types)
- Internal-only fields (marked `readOnly`, computed, or server-generated)
- Validation rules applied server-side but not documented
- RBAC roles/permissions required per operation

### 1.4 Identify the Auth Model

Determine whether the product can reuse `grafana.token` or needs separate
credentials. This directly drives `ConfigKeys()`.

Questions:
- Does the API accept the same Grafana service account token?
- Are there separate API keys or OAuth flows?
- What RBAC roles/permissions are needed? (editor, admin, product-specific?)
- Does the product require a different base URL (e.g. separate service endpoint)?

If the product uses the same Grafana token and server URL, `ConfigKeys()` can
be empty — the provider reads `curCtx.Grafana.Token` directly.

### 1.5 Map Resource Relationships

Understand how the product's resources relate to each other and to existing
grafanactl resources:

- Do resources reference each other? (e.g. reports reference SLO UUIDs)
- Is there a folder/hierarchy structure? (affects push ordering)
- Do resources reference Grafana datasources? (may need datasource resolution)
- Are there computed/derived resources? (e.g. recording rules generated from SLOs)

### 1.6 Test API Behavior

Before designing the adapter, validate assumptions by making real API calls.

**Choose a test context:**

First, identify which Grafana instance you'll test against:

```bash
# List available contexts
grafanactl config get-contexts

# Switch to the context for testing (e.g., dev, staging)
grafanactl config use-context <context-name>
```

**Using `grafanactl api`:**

Once you've selected the right context, use `grafanactl api` to test endpoints:

```bash
# List resources
grafanactl api /api/plugins/{plugin-id}/resources/v1/{resource}

# Get a specific resource by ID
grafanactl api /api/plugins/{plugin-id}/resources/v1/{resource}/{id}

# Create a resource (POST implied by -d)
grafanactl api /api/plugins/{plugin-id}/resources/v1/{resource} -d @payload.json

# Update a resource
grafanactl api /api/plugins/{plugin-id}/resources/v1/{resource}/{id} -X PUT -d @payload.json

# Delete a resource
grafanactl api /api/plugins/{plugin-id}/resources/v1/{resource}/{id} -X DELETE

# Output as YAML for easier reading
grafanactl api /api/plugins/{plugin-id}/resources/v1/{resource} -o yaml
```

**Why `grafanactl api` vs curl:**
- Uses your configured context's authentication automatically
- Supports multiple output formats (`-o json|yaml|wide`)
- Handles TLS configuration from your grafanactl config
- Less verbose than curl (no explicit token headers needed)

**Fallback to curl:**

If you need to test against an instance not yet configured in grafanactl, use curl:

```bash
# List resources
curl -H "Authorization: Bearer $TOKEN" \
  "$GRAFANA_URL/api/plugins/{plugin-id}/resources/v1/{resource}"

# Create a resource
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d @payload.json \
  "$GRAFANA_URL/api/plugins/{plugin-id}/resources/v1/{resource}"
```

**Things to verify:**
- Does the response match the OpenAPI spec?
- What happens on duplicate create? (error, update, or idempotent?)
- Are IDs server-generated, client-provided, or both?
- What fields appear in GET that weren't in POST? (these are `readOnly`)
- What error format is used? (JSON error body structure varies between products)

---

## 2. Decision Framework

Every provider needs answers to these questions before implementation starts.

### 2.1 Auth Strategy → ConfigKeys()

| Scenario | ConfigKeys | Token Source |
|----------|-----------|--------------|
| Same Grafana SA token | `[]` (empty) | `curCtx.Grafana.Token` |
| Separate product token | `[{Name: "token", Secret: true}]` | `curCtx.Providers[name]["token"]` |
| Separate URL + token | `[{Name: "url"}, {Name: "token", Secret: true}]` | Provider config |

### 2.2 API Client Type

| API Type | Client Approach | When |
|----------|----------------|------|
| Plugin API (`/api/plugins/...`) | Custom `http.Client` with Bearer token | Product is a Grafana plugin |
| K8s-compatible API (`/apis/...`) | grafanactl's existing dynamic client | Product exposes K8s-style endpoints externally |
| External service API | Custom `http.Client` with product-specific auth | Product runs outside Grafana |

Note: Some products have K8s CRDs internally that are NOT accessible externally.
Always verify with a real API call before choosing the K8s client path.

### 2.3 Envelope Mapping

Map the product's API objects to grafanactl's K8s envelope:

```
apiVersion: {group}/v1alpha1    ← convention: {product}.ext.grafana.app/v1alpha1
kind: {ResourceKind}            ← PascalCase singular (e.g. SLO, Report)
metadata:
  name: {unique-id}             ← the resource's UUID or slug
  namespace: default
spec:
  {field}: {value}              ← user-editable fields from API object
```

Decisions:
- What field maps to `metadata.name`? (UUID, slug, name?)
- Which fields are `spec` (user-editable) vs `readOnly` (server-generated)?
- What `apiVersion` group to use? Convention: `{product}.ext.grafana.app/v1alpha1`
- Should `readOnly` fields be stripped on push or preserved?

### 2.4 Command Surface

Start with the standard CRUD set, then consider product-specific additions:

```
grafanactl {provider}
├── {resource-group}           ← if multiple resource types
│   ├── list                   ← always
│   ├── get <id>               ← always
│   ├── push [path...]         ← always (create-or-update)
│   ├── pull                   ← always (export to local files)
│   ├── delete <id...>         ← always
│   └── status [id]            ← if the product has operational health data
└── {other-resource-group}
```

Questions:
- Does the product have multiple resource types? → nest under groups
- Is there operational status data? → add `status` subcommand
- Is there a preview/dry-run mode? → add `--dry-run` flag
- Can resources be validated locally? → add `validate` subcommand

### 2.5 Package Layout

```
internal/providers/{provider}/
├── provider.go                ← Provider interface impl (always)
├── provider_test.go           ← contract tests (always)
├── {resource}/                ← one subpackage per resource type
│   ├── types.go               ← Go types matching API schema
│   ├── client.go              ← HTTP client for this resource
│   ├── client_test.go
│   ├── adapter.go             ← K8s envelope ↔ API object translation
│   ├── adapter_test.go        ← round-trip property tests
│   └── commands.go            ← Cobra commands for this resource
```

Single resource type → flat package (no subpackage needed).
Multiple resource types → subpackage per type.

### 2.6 Implementation Staging

Break into stages where each stage is independently shippable and testable.
Common decomposition:

1. **Core CRUD** for the primary resource type (types + client + adapter + commands)
2. **Secondary resource types** (if any), reusing patterns from stage 1
3. **Status/monitoring commands** (if applicable), often requiring hybrid data sources
4. **Advanced features** (graphing, validation, etc.)

Each stage should produce a working `make build && make tests && make lint` result.

---

## 3. Common Pitfalls

These patterns appeared during the SLO provider exploration and are likely
to recur with other Grafana products.

| Pitfall | Description | Mitigation |
|---------|-------------|------------|
| **Incomplete OpenAPI specs** | Specs may miss endpoints, enum values, or entire query types | Cross-reference with source code route handlers |
| **K8s CRDs ≠ external APIs** | Products may register K8s CRDs internally that aren't accessible via Grafana's `/apis` endpoint | Always test with a real API call before choosing the K8s client path |
| **readOnly ≠ immutable** | Server-generated fields (status, timestamps) appear in GET but must be stripped on POST/PUT | Build adapter to separate spec (user-editable) from readOnly (server-generated) |
| **Async writes** | POST/PUT may return 202 Accepted, not 201 Created — the resource isn't fully provisioned yet | Document this behavior; don't assume immediate consistency |
| **Response wrapper variance** | Different products use different list response envelopes (`{ slos: [...] }` vs `{ items: [...] }`) | Define response types per product, don't assume a universal wrapper |
| **Lifecycle vs operational status** | An API `status` field may indicate provisioning state (creating/error), not operational health | If operational health is needed, it likely comes from Prometheus metrics, not the CRUD API |
| **Naming convention mismatch** | API uses camelCase, Terraform uses snake_case, K8s uses camelCase | Decide once per provider and document; grafanactl convention is camelCase in spec |

---

## 4. Worked Example: SLO Provider

The SLO provider was the first provider implemented using this process.
Full design docs are in [docs/designs/slo-provider/](../specs/slo-provider/).

### How SLO answered each decision

| Decision | SLO Answer | Rationale |
|----------|-----------|-----------|
| Auth | Empty `ConfigKeys()`, reuses `grafana.token` | SLO plugin accepts same SA token |
| API client | Custom `http.Client` against plugin API | K8s CRDs exist but aren't externally accessible |
| apiVersion | `slo.ext.grafana.app/v1alpha1` | `.ext.grafana.app` signals extension product |
| metadata.name | `uuid` field | SLOs are identified by UUID |
| Command surface | Nested: `slo definitions` + `slo reports` + `status` | Two resource types (SLOs, Reports) plus operational health |
| Package layout | `slo/definitions/` + `slo/reports/` | Two distinct resource types with separate types/client/adapter |
| Staging | 4 stages: CRUD → Reports → Def Status → Report Status | Each stage builds on previous, each is independently testable |

### Discovery process for SLO

1. **OpenAPI spec** from `grafana/slo-openapi-client` → mapped 11 documented endpoints
2. **Terraform provider** `grafana/terraform-provider-grafana` → extracted full SLO schema and validation rules
3. **Source code** `grafana/slo` → found 7 undocumented endpoints, discovered recording rule metrics, confirmed K8s CRDs are internal-only
4. **User feedback** → K8s APIs confirmed not externally accessible; apiVersion convention established

### Key links

- Top-level plan: [docs/designs/slo-provider/2026-03-04-slo-provider-plan.md](../specs/slo-provider/2026-03-04-slo-provider-plan.md)
- Stage 1 (Definitions CRUD): [docs/designs/slo-provider/1-slo-definitions-crud/](../specs/slo-provider/1-slo-definitions-crud/)
- Stage 2 (Reports CRUD): [docs/designs/slo-provider/2-reports-crud/](../specs/slo-provider/2-reports-crud/)
- Stage 3 (Definitions Status): [docs/designs/slo-provider/3-definitions-status/](../specs/slo-provider/3-definitions-status/)
- Stage 4 (Reports Status): [docs/designs/slo-provider/4-reports-status/](../specs/slo-provider/4-reports-status/)
