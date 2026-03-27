# Stage 1: Checks CRUD + Probes List

Parent: [Synthetic Monitoring Provider Plan](../2026-03-06-synth-provider-plan.md)

## Context

Foundation stage — all subsequent work depends on this. Establishes the SM
provider, HTTP clients, K8s envelope adapters, probe name resolution, and
CRUD commands for checks plus a read-only probes list command.

~1,200 LOC estimated.

## New Files

### `internal/providers/synth/checks/`

| File | Purpose | LOC |
|------|---------|-----|
| `types.go` | `Check`, `Label`, `CheckSettings`, response wrappers | ~120 |
| `client.go` | HTTP client: List, Get, Create, Update, Delete | ~200 |
| `client_test.go` | Unit tests with `httptest.Server` | ~200 |
| `adapter.go` | K8s envelope ↔ API translation, prepare/unprepare, probe resolution | ~200 |
| `adapter_test.go` | Round-trip property tests | ~150 |
| `commands.go` | `checks` group + list/get/push/pull subcommands | ~200 |

### `internal/providers/synth/probes/`

| File | Purpose | LOC |
|------|---------|-----|
| `types.go` | `Probe`, `ProbeCapabilities` | ~40 |
| `client.go` | HTTP client: List (read-only) | ~80 |
| `client_test.go` | Unit tests | ~80 |
| `adapter.go` | K8s envelope adapter (display only, not written to files) | ~60 |
| `adapter_test.go` | Tests | ~50 |
| `commands.go` | `probes list` subcommand | ~80 |

### `internal/providers/synth/`

| File | Purpose | LOC |
|------|---------|-----|
| `provider.go` | `SynthProvider` + `init()` + `configLoader` | ~100 |
| `provider_test.go` | Interface contract tests | ~60 |

## Modified Files

- `cmd/gcx/root/command.go`: add blank import
  `_ "github.com/grafana/gcx/internal/providers/synth"`

## Go Type Definitions

### `checks/types.go`

Field names use camelCase matching the API — ensures lossless pull → edit → push.

```go
package checks

// Check represents a Synthetic Monitoring check.
type Check struct {
    ID               int64          `json:"id,omitempty"`
    TenantID         int64          `json:"tenantId,omitempty"`
    Job              string         `json:"job"`
    Target           string         `json:"target"`
    Frequency        int64          `json:"frequency"`
    Offset           int64          `json:"offset,omitempty"`
    Timeout          int64          `json:"timeout"`
    Enabled          bool           `json:"enabled"`
    Labels           []Label        `json:"labels,omitempty"`
    Settings         CheckSettings  `json:"settings"`
    Probes           []int64        `json:"probes"`                 // IDs — only used internally; YAML uses names
    BasicMetricsOnly bool           `json:"basicMetricsOnly,omitempty"`
    AlertSensitivity string         `json:"alertSensitivity,omitempty"`
    Channels         map[string]any `json:"channels,omitempty"`
    Created          float64        `json:"created,omitempty"`
    Modified         float64        `json:"modified,omitempty"`
}

// Label is a key-value pair applied to all metrics/events for a check.
type Label struct {
    Name  string `json:"name"`
    Value string `json:"value"`
}

// CheckSettings holds check-type-specific configuration.
// Only one key is set per check (e.g. "http", "ping", "tcp").
// Using map[string]any preserves all fields without requiring Go types
// for each of the 9 check type variants and their hundreds of options.
type CheckSettings map[string]any

// CheckType returns the check type name (e.g. "http", "ping").
func (s CheckSettings) CheckType() string {
    for k := range s {
        return k
    }
    return "unknown"
}

// Tenant holds the SM tenant info needed for push operations.
type Tenant struct {
    ID int64 `json:"id"`
}

// CheckDeleteResponse is returned by DELETE /api/v1/check/delete/{id}.
type CheckDeleteResponse struct {
    Msg     string `json:"msg"`
    CheckID int64  `json:"checkId"`
}
```

### `probes/types.go`

```go
package probes

// Probe represents a Synthetic Monitoring probe node.
type Probe struct {
    ID           int64              `json:"id"`
    TenantID     int64              `json:"tenantId"`
    Name         string             `json:"name"`
    Latitude     float64            `json:"latitude"`
    Longitude    float64            `json:"longitude"`
    Labels       []ProbeLabel       `json:"labels,omitempty"`
    Region       string             `json:"region"`
    Public       bool               `json:"public"`
    Online       bool               `json:"online"`
    OnlineChange float64            `json:"onlineChange"`
    Version      string             `json:"version"`
    Deprecated   bool               `json:"deprecated"`
    Created      float64            `json:"created"`
    Modified     float64            `json:"modified"`
    Capabilities ProbeCapabilities  `json:"capabilities"`
}

type ProbeLabel struct {
    Name  string `json:"name"`
    Value string `json:"value"`
}

type ProbeCapabilities struct {
    DisableScriptedChecks bool `json:"disableScriptedChecks"`
    DisableBrowserChecks  bool `json:"disableBrowserChecks"`
}
```

## Envelope Translation

### Check (Pull direction: API → K8s file)

```
Check API JSON                  K8s Envelope
--------------                  ------------
id            ──────────────►   metadata.name (strconv.FormatInt)
tenantId      ────── STRIPPED   (fetched at push time via GET /api/v1/tenant)
job           ──────────────►   spec.job
target        ──────────────►   spec.target
frequency     ──────────────►   spec.frequency
timeout       ──────────────►   spec.timeout
enabled       ──────────────►   spec.enabled
labels        ──────────────►   spec.labels
settings      ──────────────►   spec.settings
probes [166,217] ───(resolve)►  spec.probes ["Oregon", "Spain"]
basicMetricsOnly ───────────►   spec.basicMetricsOnly
alertSensitivity ───────────►   spec.alertSensitivity
created       ────── STRIPPED   (server-managed)
modified      ────── STRIPPED   (server-managed)
channels      ────── STRIPPED   (not user-configurable)
```

Added by adapter:
- `apiVersion: syntheticmonitoring.ext.grafana.app/v1alpha1`
- `kind: Check`
- `metadata.namespace`: from config context

File path convention: `checks/{id}.yaml`

### Check (Push direction: K8s file → API)

1. Read `metadata.name`
2. If name parses as int64 → **update**: set `id` + fetch/inject `tenantId`
3. If name does not parse as int64 → **create**: POST without `id`
4. Resolve `spec.probes` (names) → `probes` (IDs) via probe list cache
5. After create: update local file `metadata.name` to server-assigned numeric ID

### Example YAML (after pull)

```yaml
apiVersion: syntheticmonitoring.ext.grafana.app/v1alpha1
kind: Check
metadata:
  name: "6247"
  namespace: default
spec:
  job: "Mimir: mimir-dev-10 GET root"
  target: https://prometheus-dev-10-dev-eu-west-2.grafana-dev.net
  frequency: 60000
  timeout: 10000
  enabled: true
  labels:
    - name: team
      value: mimir
  settings:
    http:
      ipVersion: V4
      method: GET
      noFollowRedirects: true
      failIfSSL: false
      failIfNotSSL: true
      failIfBodyNotMatchesRegexp:
        - OK
  probes:
    - Oregon
    - Spain
  basicMetricsOnly: true
  alertSensitivity: none
```

### Example YAML (new check, before push)

```yaml
apiVersion: syntheticmonitoring.ext.grafana.app/v1alpha1
kind: Check
metadata:
  name: my-grafana-check      # non-numeric → create on push
  namespace: default
spec:
  job: grafana-com-health
  target: https://grafana.com
  frequency: 60000
  timeout: 10000
  enabled: true
  settings:
    http:
      method: GET
      ipVersion: V4
  probes:
    - Oregon
```

After `gcx synth checks push my-grafana-check.yaml`:
- File `metadata.name` updated to `"8127"` in place
- Output: `Check 'grafana-com-health' created (id=8127).`

## Push Idempotency

```
push flow:
1. Read YAML files → FromResource() → []Check
2. Fetch tenant ID once: GET /api/v1/tenant → cache
3. Load probe list once: GET /api/v1/probe/list → build name→ID map
4. For each check:
   a. Resolve probe names → IDs (error if any not found)
   b. If metadata.name is numeric → update (POST /api/v1/check/update with id+tenantId)
   c. Else → create (POST /api/v1/check/add)
   d. On create: update local YAML metadata.name with new ID
5. Report summary
```

## HTTP Client Design

`checks/client.go` provides `Client` with methods:
- `List(ctx) ([]Check, error)`
- `Get(ctx, id int64) (*Check, error)`
- `Create(ctx, check Check) (*Check, error)`
- `Update(ctx, check Check) (*Check, error)`
- `Delete(ctx, id int64) error`
- `GetTenant(ctx) (*Tenant, error)`  — used once per push to get tenantID

`probes/client.go` provides `Client` with:
- `List(ctx) ([]Probe, error)`

Both clients accept `baseURL, token string` as constructor args:
```go
func NewClient(baseURL, token string) *Client
```

## Error Handling

```
Status Code → Exit Behavior:
  401/403 → exit 3: "authentication failed: check GRAFANA_SM_TOKEN or run gcx config set"
  404     → exit 1: "check <id> not found"
  400     → exit 2: include API error message body
  500     → exit 1: include response body
  partial → exit 4: "N checks pushed, M failed" (continue on error, report all failures)
```

## Implementation Order

1. `checks/types.go` — API types
2. `probes/types.go` — probe types
3. `checks/client.go` + `client_test.go` — checks HTTP client (httptest)
4. `probes/client.go` + `client_test.go` — probes HTTP client
5. `checks/adapter.go` + `adapter_test.go` — round-trip + probe name resolution
6. `probes/adapter.go` + `adapter_test.go`
7. `checks/commands.go` — list/get/push/pull
8. `probes/commands.go` — probes list
9. `provider.go` + `provider_test.go` — wire it all together
10. `cmd/gcx/root/command.go` — blank import

## Verification

```bash
make lint && make tests && make build
bin/gcx synth --help
bin/gcx providers
bin/gcx config view   # sm_token must appear as [REDACTED]
```

Live smoke tests (load credentials from `.env`):
```bash
source .env

# Configure a context with SM credentials
bin/gcx config set providers.synth.sm-url "$GRAFANA_SM_URL"
bin/gcx config set providers.synth.sm-token "$GRAFANA_SM_TOKEN"

# Probes
bin/gcx synth probes list
bin/gcx synth probes list -o json

# Checks — read path
bin/gcx synth checks list
bin/gcx synth checks list -o json
bin/gcx synth checks list -o yaml
FIRST_ID=$(bin/gcx synth checks list -o json | jq -r '.[0].metadata.name')
bin/gcx synth checks get "$FIRST_ID"

# Pull round-trip
mkdir -p /tmp/synth-smoke
bin/gcx synth checks pull --output /tmp/synth-smoke
ls /tmp/synth-smoke/checks/       # should contain <id>.yaml files
cat /tmp/synth-smoke/checks/"$FIRST_ID".yaml   # probe names, no tenantId/id in spec

# Push round-trip (update existing check, expect idempotent)
bin/gcx synth checks push /tmp/synth-smoke/checks/"$FIRST_ID".yaml

# Create + delete (new check smoke test)
cat > /tmp/synth-smoke/new-check.yaml << 'EOF'
apiVersion: syntheticmonitoring.ext.grafana.app/v1alpha1
kind: Check
metadata:
  name: smoke-test-grafana-com
  namespace: default
spec:
  job: smoke-test-grafana-com
  target: https://grafana.com
  frequency: 120000
  timeout: 10000
  enabled: true
  settings:
    http:
      method: GET
      ipVersion: V4
  probes:
    - Oregon
EOF
bin/gcx synth checks push /tmp/synth-smoke/new-check.yaml
# metadata.name in file should now be numeric (e.g. "8200")
cat /tmp/synth-smoke/new-check.yaml
NEW_ID=$(grep 'name:' /tmp/synth-smoke/new-check.yaml | awk '{print $2}')
bin/gcx synth checks delete "$NEW_ID"   # clean up
```
