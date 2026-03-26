# Audit Artifact Templates

Copy each template and fill in concrete values. No placeholders allowed in
the final artifacts — use actual resource names, field names, and context names.

---

## Artifact 1: Parity Table

```markdown
## Parity Table: {provider} ({cloud CLI source path})

| Cloud CLI command | grafanactl equivalent | status | notes |
|-------------|-----------------------|--------|-------|
| cloud-cli {resource} list | grafanactl {resource} list | Implemented | Maps to adapter ListFn |
| cloud-cli {resource} get {id} | grafanactl {resource} get {id} | Implemented | Maps to adapter GetFn |
| cloud-cli {resource} create | grafanactl {resource} create | Implemented | Maps to adapter CreateFn |
| cloud-cli {resource} update {id} | grafanactl {resource} update {id} | Implemented | Maps to adapter UpdateFn |
| cloud-cli {resource} delete {id} | grafanactl {resource} delete {id} | Implemented | Maps to adapter DeleteFn |
| cloud-cli {resource} {subcommand} | grafanactl {resource} {subcommand} | Deferred / N/A | {reason} |

Status values: Implemented | Deferred | N/A
```

---

## Artifact 2: Architectural Mapping

```markdown
## Architectural Mapping: {provider}

### (a) Cloud CLI flat client -> TypedCRUD[T] adapter

Cloud CLI pattern:
  type Client struct { *grafana.Client }
  func (c *Client) ListResources(ctx) ([]T, error) { c.Get(...) }

grafanactl translation:
  adapter.TypedCRUD[{ResourceType}]{
    ListFn:   client.List,
    GetFn:    client.Get,
    CreateFn: client.Create,
    UpdateFn: client.Update,
    DeleteFn: client.Delete,
    NameFn:   func(r {ResourceType}) string { return r.{UID field} },
  }

Notes: {any provider-specific adaptations, e.g. int->string ID mapping}

### (b) Cloud CLI flags -> Options struct with setup/Validate

Cloud CLI pattern:
  cmd.Flags().StringVar(&opts.Filter, "filter", "", "...")
  // ad-hoc validation inline in RunE

grafanactl translation:
  type {Resource}Opts struct { Filter string }
  func (o *{Resource}Opts) setup(cmd *cobra.Command) { ... }
  func (o *{Resource}Opts) Validate() error { ... }

Notes: {list each flag that needs translation}

### (c) Cloud CLI output formatting -> codec registry with K8s envelope

Cloud CLI pattern:
  json.Marshal(result) / fmt.Printf table directly

grafanactl translation:
  codec.Encode(resources, opts.Output) where resources is []*Resource
  wrapped in K8s envelope: TypeMeta{Kind, APIVersion} + ObjectMeta{Name}
  Output modes: table (default), wide, json, yaml

Notes: {any fields used as table columns, any wide-only columns}

### (d) Cloud CLI types -> Go structs with omitzero

Cloud CLI pattern:
  type Resource struct { Field *string `json:"field,omitempty"` }

grafanactl translation:
  type Resource struct { Field string `json:"field,omitzero"` }
  (Go 1.24+ omitzero replaces omitempty for struct-typed fields)

Notes: {list any FlexTime or special zero-value fields}

### (e) Cloud CLI provider registration -> adapter.Register() in init() with blank import

Cloud CLI pattern:
  // registration in main package or explicit wire-up

grafanactl translation:
  // internal/providers/{name}/provider.go
  func init() {
    providers.Register(&Provider{})
    {resource}.Register(&configLoader{})
  }
  // cmd/grafanactl/root/command.go
  _ "github.com/grafana/grafanactl/internal/providers/{name}"

Notes: {ConfigKeys required: [] for same SA token, [{Name: "url"}, {Name: "token"}] for separate}
```

---

## Artifact 3: Verification Plan

```markdown
## Verification Plan: {provider}

### Automated Tests

1. Client HTTP tests (`{resource}/client_test.go`):
   - `Test{Resource}Client_List` -- httptest server returning known JSON fixture,
     verify all fields parse correctly
   - `Test{Resource}Client_Get` -- httptest returning single resource, verify
     fields including nested structs
   - `Test{Resource}Client_Create` -- verify request body and response parsing
   - `Test{Resource}Client_Error` -- 4xx/5xx responses produce wrapped errors

2. Adapter round-trip tests (`{resource}/adapter_test.go`):
   - `Test{Resource}AdapterRoundTrip` -- create typed object -> adapter wraps to
     Resource -> unwrap back -> compare all fields (no data loss)

3. TypedCRUD interface compliance:
   - Compilation gate: if `adapter.TypedCRUD[{ResourceType}]` does not satisfy
     the `ResourceAdapter` interface, `make build` will catch it

### Smoke Test Commands

Run every command with CTX={context-name} against the live instance.

```bash
CTX={context-name}  # fill in before running

# --- List: compare resource IDs ---
# Run cloud CLI: cloud-cli --context=$CTX {resource} list -o json | jq -r '.[].{id_field}' | sort
LEGACY_IDS=$(...)  # substitute actual cloud CLI command
GCTL_IDS=$(grafanactl --context=$CTX {resource} list -o json | jq -r '.[].metadata.name' | sort)
echo "=== List ID diff ===" && diff <(echo "$LEGACY_IDS") <(echo "$GCTL_IDS") && echo "MATCH" || echo "MISMATCH"

# --- Get: compare key fields ---
ID="{pick a real ID from list output}"
# Run cloud CLI: cloud-cli --context=$CTX {resource} get $ID -o json | jq '{title: .{title_field}, status: .{status_field}}' > /tmp/legacy_get.json
grafanactl --context=$CTX {resource} get $ID -o json \
  | jq '{title: .spec.{title_field}, status: .spec.{status_field}}' > /tmp/gctl_get.json
echo "=== Get field diff ===" && diff /tmp/legacy_get.json /tmp/gctl_get.json && echo "MATCH" || echo "MISMATCH"

# --- Adapter path ---
grafanactl --context=$CTX resources get {alias} > /dev/null 2>&1 && echo "resources get: OK" || echo "resources get: FAIL"
grafanactl --context=$CTX resources get {alias}/$ID -o json > /dev/null 2>&1 && echo "resources get/id: OK" || echo "resources get/id: FAIL"

# --- Ancillary subcommands (one block per non-CRUD subcommand) ---
echo "=== Ancillary: {subcommand} ===" && \
# cloud-cli --context=$CTX {resource} {subcommand} -o json | jq length
# (substitute actual cloud CLI command above)
grafanactl --context=$CTX {resource} {subcommand} -o json | jq length

# --- Output format check ---
for fmt in table wide json yaml; do
  GRAFANACTL_AGENT_MODE=false grafanactl --context=$CTX {resource} list -o $fmt > /dev/null 2>&1 \
    && echo "$fmt: OK" || echo "$fmt: FAIL"
done
```

### Build Gate Checkpoints

Run `GRAFANACTL_AGENT_MODE=false make all` at these points:
1. After Step 2 (types.go) -- verify compilation
2. After Step 3 (client.go) -- verify lint passes
3. After Step 4 (adapter.go) -- verify TypedCRUD wiring compiles
4. After Step 6 (tests) -- verify all tests pass
5. **Final gate** before Stage 3: `GRAFANACTL_AGENT_MODE=false make all` must
   exit 0 with no lint errors, all tests passing, and docs regenerated.
```
