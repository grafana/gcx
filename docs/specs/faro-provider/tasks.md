---
type: feature-tasks
title: "Faro Provider Implementation Tasks"
status: draft
spec: docs/specs/faro-provider/spec.md
plan: docs/specs/faro-provider/plan.md
created: 2026-04-02
---

# Implementation Tasks

## Dependency Graph

```
T1 (types.go)
 └──→ T2 (client.go + client_test.go)
       └──→ T3 (resource_adapter.go + resource_adapter_test.go + types_identity_test.go)
             └──→ T4 (provider.go + commands.go + commands_test.go)
                   └──→ T5 (sourcemap commands)
                         └──→ T6 (integration wiring + make all)
                               └──→ T7 (smoke tests — all 4 formats)
                                     └──→ T8 (adapter smoke + spec compliance)
```

## Wave 1: Core Types and Client

### T1: Types

**Priority**: P0
**Effort**: Small
**Depends on**: none
**Type**: task

Port type definitions from `pkg/grafana/faro/faro.go`. Includes:
- `FaroApp` (public domain type — string ID, map ExtraLogLabels)
- `faroAppAPI` (internal wire type — int64 ID, array ExtraLogLabels)
- `CORSOrigin`, `LogLabel`, `FaroAppSettings`
- `toAPI()` / `fromAPI()` conversion functions
- `ResourceIdentity` implementation on FaroApp (`GetResourceName`/`SetResourceName` using `adapter.SlugifyName`/`ExtractIDFromSlug`)

**Deliverables:**
- `internal/providers/faro/types.go`

**Acceptance criteria:**
- GIVEN the types are defined WHEN FaroApp.GetResourceName() is called THEN it returns slug-id format
- GIVEN a slug-id string WHEN SetResourceName() is called THEN the numeric ID is extracted and stored

---

### T2: HTTP Client

**Priority**: P0
**Effort**: Medium
**Depends on**: T1
**Type**: task

Port client from `pkg/grafana/faro/faro.go`. Translate embedded
`grafana.Client` to explicit `http.Client` + `host` fields with named
endpoint methods.

Methods: `List`, `Get`, `GetByName`, `Create`, `Update`, `Delete`.

API quirks to preserve:
- Create strips ExtraLogLabels + Settings, re-fetches via List
- Update strips Settings, requires ID in URL + body
- No retries on mutations
- ExtraLogLabels map↔array conversion via toAPI/fromAPI

Also port sourcemap client methods: `ListSourcemaps`, `UploadSourcemap`,
`DeleteSourcemap` using the separate plugin resource base path.

**Deliverables:**
- `internal/providers/faro/client.go`
- `internal/providers/faro/client_test.go`

**Acceptance criteria:**
- GIVEN a httptest server returning `[]faroAppAPI` JSON WHEN client.List() is called THEN it returns `[]FaroApp` with correct field conversion
- GIVEN a httptest server WHEN client.Create() is called THEN ExtraLogLabels and Settings are NOT in the request body
- GIVEN a httptest server WHEN client.Update() is called THEN Settings is NOT in the request body AND ID is in both URL and body
- GIVEN a httptest server WHEN client.Get() is called with a numeric ID THEN the correct path is constructed
- GIVEN a httptest server returning sourcemap bundles WHEN client.ListSourcemaps() is called THEN the response is decoded correctly

---

## Wave 2: Adapter and Registration

### T3: Resource Adapter

**Priority**: P0
**Effort**: Medium
**Depends on**: T2
**Type**: task

Wire `TypedCRUD[FaroApp]` adapter factory with Descriptor, GVK, Schema,
and Example. Implement the adapter factory function that constructs the
client from `ConfigLoader.LoadGrafanaConfig(ctx)`.

GVK: `faro.ext.grafana.app/v1alpha1/FaroApp`
Plural: `faroapps`

**Deliverables:**
- `internal/providers/faro/resource_adapter.go`
- `internal/providers/faro/resource_adapter_test.go`
- `internal/providers/faro/types_identity_test.go`

**Acceptance criteria:**
- GIVEN a FaroApp WHEN converted to unstructured and back THEN no data is lost (round-trip test)
- GIVEN the adapter is registered WHEN `SchemaForGVK` is called with the FaroApp GVK THEN a non-nil schema is returned
- GIVEN the adapter is registered WHEN `ExampleForGVK` is called THEN a non-nil example is returned
- GIVEN ResourceIdentity tests WHEN GetResourceName/SetResourceName round-trip THEN slug-id is preserved

---

## Wave 3: Provider and CLI Commands

### T4: Provider Registration and CRUD Commands

**Priority**: P0
**Effort**: Medium-Large
**Depends on**: T3
**Type**: task

Implement `FaroProvider` struct (Provider interface) and all 5 CRUD commands
under `faro apps`.

Provider: `Name()="faro"`, `ShortDesc()="Manage Grafana Frontend Observability (Faro) resources."`,
`ConfigKeys()=nil`, `Validate()=nil`.

Commands use `TypedCRUD[FaroApp]` typed methods for data access (not raw
client). Register table + wide codecs. DefaultFormat("text").

Table columns: NAME (slug-id), APP KEY, COLLECT ENDPOINT URL
Wide columns: + CORS ORIGINS, EXTRA LOG LABELS, GEOLOCATION

**Deliverables:**
- `internal/providers/faro/provider.go`
- `internal/providers/faro/commands.go`
- `internal/providers/faro/commands_test.go`

**Acceptance criteria:**
- GIVEN the provider is registered WHEN Provider.Name() is called THEN it returns "faro"
- GIVEN no config keys WHEN Provider.ConfigKeys() is called THEN it returns nil
- GIVEN a list command WHEN run with default flags THEN output is in table format
- GIVEN a list command WHEN run with `-o json` THEN output is K8s-envelope wrapped
- GIVEN a list command WHEN run with `-o wide` THEN additional columns are shown
- GIVEN a create command WHEN run without `-f` THEN an error is returned

---

### T5: Sourcemap Sub-Resource Commands

**Priority**: P1
**Effort**: Medium
**Depends on**: T4
**Type**: task

Add `show-sourcemaps`, `apply-sourcemap`, `remove-sourcemap` as verbs under
the `apps` subcommand group. These use the raw client (not TypedCRUD) since
sourcemaps are not adapter-registered.

Sourcemap commands use the separate plugin resource base path:
`/api/plugins/grafana-kowalski-app/resources/api/v1/app/{id}/sourcemaps`

**Deliverables:**
- Additions to `internal/providers/faro/commands.go` (sourcemap command functions)
- Additions to `internal/providers/faro/commands_test.go` (sourcemap command tests)

**Acceptance criteria:**
- GIVEN a Faro app exists WHEN `faro apps show-sourcemaps <slug-id>` is run THEN sourcemap bundles are listed
- GIVEN a valid sourcemap file WHEN `faro apps apply-sourcemap <slug-id> -f bundle.json` is run THEN the bundle is uploaded
- GIVEN a sourcemap bundle exists WHEN `faro apps remove-sourcemap <slug-id> <bundle-id>` is run THEN the bundle is deleted
- GIVEN sourcemap commands WHEN checked against adapter registry THEN they are NOT registered as typed adapters

---

## Wave 4: Integration and Verification

### T6: Integration Wiring

**Priority**: P0
**Effort**: Small
**Depends on**: T5
**Type**: chore

Wire the provider into the CLI:
1. Add blank import in `cmd/gcx/root/command.go`
2. Fix any import cycles or variable name collisions
3. Run `GCX_AGENT_MODE=false make all` — must exit 0

**Deliverables:**
- Modification to `cmd/gcx/root/command.go` (blank import)
- `GCX_AGENT_MODE=false make all` passes

**Acceptance criteria:**
- GIVEN the blank import is added WHEN `GCX_AGENT_MODE=false make all` is run THEN it exits 0
- GIVEN the build succeeds WHEN `gcx providers` is run THEN `faro` appears in the list

---

### T7: Smoke Tests — All Output Formats

**Priority**: P0
**Effort**: Medium
**Depends on**: T6
**Type**: task

Run every list/get command with all 4 output formats (`-o json`, `-o table`,
`-o wide`, `-o yaml`). Verify non-empty output and correct structure.

```bash
CTX={context-name}

# FaroApp CRUD smoke
for fmt in json table wide yaml; do
  GCX_AGENT_MODE=false gcx --context=$CTX faro apps list -o $fmt > /dev/null 2>&1 \
    && echo "apps list $fmt: OK" || echo "apps list $fmt: FAIL"
done

for fmt in json table wide yaml; do
  GCX_AGENT_MODE=false gcx --context=$CTX faro apps get <slug-id> -o $fmt > /dev/null 2>&1 \
    && echo "apps get $fmt: OK" || echo "apps get $fmt: FAIL"
done

# Sourcemap smoke
GCX_AGENT_MODE=false gcx --context=$CTX faro apps show-sourcemaps <slug-id> \
  && echo "show-sourcemaps: OK" || echo "show-sourcemaps: FAIL"
```

**Deliverables:**
- All commands produce non-error output in all 4 formats
- Smoke test results documented

**Acceptance criteria:**
- GIVEN a live Grafana instance WHEN each list/get command is run with each of json/table/wide/yaml THEN all produce non-empty, correctly structured output
- GIVEN a live Grafana instance WHEN sourcemap commands are run THEN they succeed or return expected empty results

---

### T8: Adapter Smoke and Spec Compliance

**Priority**: P0
**Effort**: Small
**Depends on**: T7
**Type**: task

Verify adapter path and check every acceptance criterion from spec.md.

```bash
CTX={context-name}

# Adapter smoke
GCX_AGENT_MODE=false gcx --context=$CTX resources schemas | grep -i faro
GCX_AGENT_MODE=false gcx --context=$CTX resources get faroapps
GCX_AGENT_MODE=false gcx --context=$CTX resources get faroapps/<slug-id>
```

Produce comparison report: provider path vs adapter path json output must be
identical. Check every AC from spec.md and report SATISFIED/UNSATISFIED.

Update `gcx-provider-recipe.md` with:
1. Status tracker entry for faro
2. Gotchas discovered during smoke tests
3. Pattern corrections (if any)

**Deliverables:**
- Comparison report (provider vs adapter json output)
- Spec compliance checklist (all ACs)
- Recipe update

**Acceptance criteria:**
- GIVEN `faro apps list -o json` and `resources get faroapps -o json` WHEN outputs are compared THEN they are structurally identical
- GIVEN the spec.md acceptance criteria WHEN each is checked THEN all are SATISFIED
- GIVEN the recipe WHEN updated THEN faro status row and any gotchas are documented
