# Grafana Cloud REST Config Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a shared "Grafana Cloud" auth layer to the config system so cloud providers (fleet, oncall, k6) authenticate with a single access policy token and auto-discover service URLs via GCOM.

**Architecture:** Extend `Context` with a `CloudConfig` (token + stack slug + api-url). Add a GCOM client in `internal/cloud/` that discovers stack info. Extend `providers.ConfigLoader` with `LoadCloudConfig` alongside the renamed `LoadGrafanaConfig`. Fleet provider refactored to use cloud config instead of per-provider URL/token.

**Tech Stack:** Go, `net/http` (GCOM client), `internal/config` (types/editor), `internal/providers` (ConfigLoader)

---

## Dependency Graph

```
T1 (CloudConfig type)
├─→ T2 (GCOM client)
│   └─→ T3 (LoadCloudConfig on ConfigLoader)
│       └─→ T5 (Refactor fleet to use cloud config)
└─→ T4 (Rename LoadRESTConfig → LoadGrafanaConfig)
    └─→ T5
```

---

### Task 1: Add CloudConfig to Context

**Files:**
- Modify: `internal/config/types.go` (Context struct, CloudConfig struct)
- Modify: `internal/config/types_test.go` (if exists, add cloud config tests)
- Test: `internal/config/editor_test.go` (add test for `contexts.X.cloud.token` path)

**Step 1: Write the failing test**

Add to `internal/config/editor_test.go`:

```go
{
    name: "set cloud token on new context",
    path: "contexts.new.cloud.token",
    value: "cloud-abc123",
    want: func() Config {
        cfg := Config{Contexts: map[string]*Context{}}
        cfg.Contexts["new"] = &Context{
            Cloud: &CloudConfig{Token: "cloud-abc123"},
        }
        return cfg
    }(),
},
{
    name: "set cloud api-url",
    path: "contexts.new.cloud.api-url",
    value: "grafana-dev.com",
    want: func() Config {
        cfg := Config{Contexts: map[string]*Context{}}
        cfg.Contexts["new"] = &Context{
            Cloud: &CloudConfig{APIUrl: "grafana-dev.com"},
        }
        return cfg
    }(),
},
{
    name: "set cloud stack",
    path: "contexts.new.cloud.stack",
    value: "mystack",
    want: func() Config {
        cfg := Config{Contexts: map[string]*Context{}}
        cfg.Contexts["new"] = &Context{
            Cloud: &CloudConfig{Stack: "mystack"},
        }
        return cfg
    }(),
},
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestSetValue -v`
Expected: FAIL — `CloudConfig` type does not exist

**Step 3: Add CloudConfig type and Context field**

In `internal/config/types.go`, add after `GrafanaConfig`:

```go
// CloudConfig holds Grafana Cloud authentication settings.
// Used by cloud providers (fleet, oncall, k6) that authenticate via
// Grafana Cloud access policy tokens rather than Grafana instance tokens.
type CloudConfig struct {
    // Token is a Grafana Cloud access policy token.
    // Required for cloud provider operations.
    Token string `datapolicy:"secret" env:"GRAFANA_CLOUD_TOKEN" json:"token,omitempty" yaml:"token,omitempty"`

    // Stack is the Grafana Cloud stack slug (e.g., "mystack").
    // Optional — derived from grafana.server URL if not set.
    Stack string `env:"GRAFANA_CLOUD_STACK" json:"stack,omitempty" yaml:"stack,omitempty"`

    // APIUrl is the GCOM API URL for stack discovery.
    // Defaults to "grafana.com". Override for dev/staging environments.
    APIUrl string `env:"GRAFANA_CLOUD_API_URL" json:"api-url,omitempty" yaml:"api-url,omitempty"`
}
```

Add to `Context` struct:

```go
type Context struct {
    Name    string         `json:"-" yaml:"-"`
    Grafana *GrafanaConfig `json:"grafana,omitempty" yaml:"grafana,omitempty"`
    Cloud   *CloudConfig   `json:"cloud,omitempty" yaml:"cloud,omitempty"`  // NEW
    // ... rest unchanged
}
```

Add a helper to derive stack slug from grafana server URL:

```go
// DefaultGCOMURL is the default GCOM API URL.
const DefaultGCOMURL = "https://grafana.com"

// ResolveStackSlug returns the cloud stack slug.
// If Cloud.Stack is set, it is returned directly.
// Otherwise, attempts to derive from Grafana.Server URL
// (e.g., "mystack" from "https://mystack.grafana.net").
func (ctx *Context) ResolveStackSlug() string {
    if ctx.Cloud != nil && ctx.Cloud.Stack != "" {
        return ctx.Cloud.Stack
    }
    if ctx.Grafana == nil || ctx.Grafana.Server == "" {
        return ""
    }
    u, err := url.Parse(ctx.Grafana.Server)
    if err != nil {
        return ""
    }
    host := u.Hostname()
    // Extract slug from *.grafana.net or *.grafana-dev.net patterns.
    for _, suffix := range []string{".grafana.net", ".grafana-dev.net"} {
        if strings.HasSuffix(host, suffix) {
            return strings.TrimSuffix(host, suffix)
        }
    }
    return ""
}

// ResolveGCOMURL returns the GCOM API URL, defaulting to grafana.com.
func (ctx *Context) ResolveGCOMURL() string {
    if ctx.Cloud != nil && ctx.Cloud.APIUrl != "" {
        return "https://" + ctx.Cloud.APIUrl
    }
    return DefaultGCOMURL
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -run TestSetValue -v`
Expected: PASS

**Step 5: Add unit tests for ResolveStackSlug and ResolveGCOMURL**

Create test cases in `internal/config/types_test.go`:

```go
func TestContext_ResolveStackSlug(t *testing.T) {
    tests := []struct {
        name string
        ctx  config.Context
        want string
    }{
        {
            name: "explicit cloud stack",
            ctx:  config.Context{Cloud: &config.CloudConfig{Stack: "mystack"}},
            want: "mystack",
        },
        {
            name: "derived from grafana.net URL",
            ctx:  config.Context{Grafana: &config.GrafanaConfig{Server: "https://mystack.grafana.net"}},
            want: "mystack",
        },
        {
            name: "derived from grafana-dev.net URL",
            ctx:  config.Context{Grafana: &config.GrafanaConfig{Server: "https://dev.grafana-dev.net"}},
            want: "dev",
        },
        {
            name: "explicit overrides derived",
            ctx: config.Context{
                Cloud:   &config.CloudConfig{Stack: "explicit"},
                Grafana: &config.GrafanaConfig{Server: "https://derived.grafana.net"},
            },
            want: "explicit",
        },
        {
            name: "custom domain returns empty",
            ctx:  config.Context{Grafana: &config.GrafanaConfig{Server: "https://grafana.mycompany.com"}},
            want: "",
        },
        {
            name: "no config returns empty",
            ctx:  config.Context{},
            want: "",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            assert.Equal(t, tt.want, tt.ctx.ResolveStackSlug())
        })
    }
}
```

**Step 6: Run all config tests**

Run: `go test ./internal/config/ -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/config/types.go internal/config/types_test.go internal/config/editor_test.go
git commit -m "feat(config): add CloudConfig to Context for Grafana Cloud auth"
```

---

### Task 2: Add GCOM client

**Files:**
- Create: `internal/cloud/gcom.go`
- Create: `internal/cloud/gcom_test.go`

**Step 1: Write the failing test**

```go
package cloud_test

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/grafana/grafanactl/internal/cloud"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestGCOMClient_GetStack(t *testing.T) {
    tests := []struct {
        name    string
        slug    string
        handler http.HandlerFunc
        wantID  int
        wantErr bool
    }{
        {
            name: "returns stack info",
            slug: "mystack",
            handler: func(w http.ResponseWriter, r *http.Request) {
                assert.Equal(t, "/api/instances/mystack", r.URL.Path)
                assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
                w.Header().Set("Content-Type", "application/json")
                json.NewEncoder(w).Encode(cloud.StackInfo{
                    ID:                        123,
                    Slug:                      "mystack",
                    AgentManagementInstanceID:  456,
                    AgentManagementInstanceURL: "https://fleet.example.com",
                })
            },
            wantID: 123,
        },
        {
            name: "returns error on 404",
            slug: "missing",
            handler: func(w http.ResponseWriter, _ *http.Request) {
                w.WriteHeader(http.StatusNotFound)
            },
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            server := httptest.NewServer(tt.handler)
            defer server.Close()

            client := cloud.NewGCOMClient(server.URL, "test-token")
            stack, err := client.GetStack(t.Context(), tt.slug)

            if tt.wantErr {
                require.Error(t, err)
                return
            }

            require.NoError(t, err)
            assert.Equal(t, tt.wantID, stack.ID)
        })
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/cloud/ -v`
Expected: FAIL — package does not exist

**Step 3: Implement GCOM client**

Create `internal/cloud/gcom.go`:

```go
// Package cloud provides clients for Grafana Cloud platform APIs.
package cloud

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "strings"
    "time"
)

// StackInfo contains information about a Grafana Cloud stack,
// discovered via the GCOM API. Ported from gcx pkg/grafana/gcom.
type StackInfo struct {
    ID                          int    `json:"id"`
    Slug                        string `json:"slug"`
    Name                        string `json:"name"`
    URL                         string `json:"url"`
    OrgID                       int    `json:"orgId"`
    OrgSlug                     string `json:"orgSlug"`
    Status                      string `json:"status"`
    RegionSlug                  string `json:"regionSlug"`

    // Prometheus (Hosted Metrics)
    HMInstancePromID        int    `json:"hmInstancePromId"`
    HMInstancePromURL       string `json:"hmInstancePromUrl"`
    HMInstancePromClusterID int    `json:"hmInstancePromClusterId"`

    // Loki (Hosted Logs)
    HLInstanceID  int    `json:"hlInstanceId"`
    HLInstanceURL string `json:"hlInstanceUrl"`

    // Tempo (Hosted Traces)
    HTInstanceID  int    `json:"htInstanceId"`
    HTInstanceURL string `json:"htInstanceUrl"`

    // Pyroscope (Hosted Profiles)
    HPInstanceID  int    `json:"hpInstanceId"`
    HPInstanceURL string `json:"hpInstanceUrl"`

    // Fleet Management (Agent Management)
    AgentManagementInstanceID   int    `json:"agentManagementInstanceId"`
    AgentManagementInstanceURL  string `json:"agentManagementInstanceUrl"`
    AgentManagementInstanceName string `json:"agentManagementInstanceName"`

    // Alertmanager
    AMInstanceID  int    `json:"amInstanceId"`
    AMInstanceURL string `json:"amInstanceUrl"`
}

// GCOMClient is a client for the Grafana.com (GCOM) API.
type GCOMClient struct {
    baseURL string
    token   string
    http    *http.Client
}

// NewGCOMClient creates a new GCOM client.
func NewGCOMClient(baseURL, token string) *GCOMClient {
    return &GCOMClient{
        baseURL: strings.TrimRight(baseURL, "/"),
        token:   token,
        http:    &http.Client{Timeout: 30 * time.Second},
    }
}

// GetStack retrieves information about a Grafana Cloud stack by slug.
func (c *GCOMClient) GetStack(ctx context.Context, slug string) (*StackInfo, error) {
    reqURL := c.baseURL + "/api/instances/" + url.PathEscape(slug)
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
    if err != nil {
        return nil, fmt.Errorf("gcom: create request: %w", err)
    }
    req.Header.Set("Authorization", "Bearer "+c.token)
    req.Header.Set("Accept", "application/json")

    resp, err := c.http.Do(req)
    if err != nil {
        return nil, fmt.Errorf("gcom: get stack %s: %w", slug, err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("gcom: get stack %s: status %d: %s", slug, resp.StatusCode, string(body))
    }

    var stack StackInfo
    if err := json.NewDecoder(resp.Body).Decode(&stack); err != nil {
        return nil, fmt.Errorf("gcom: get stack %s: decode: %w", slug, err)
    }

    return &stack, nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/cloud/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cloud/gcom.go internal/cloud/gcom_test.go
git commit -m "feat(cloud): add GCOM client for stack info discovery"
```

---

### Task 3: Add LoadCloudConfig to providers.ConfigLoader

**Files:**
- Modify: `internal/providers/configloader.go`
- Create: `internal/providers/configloader_test.go`

**Step 1: Write the failing test**

In `internal/providers/configloader_test.go`, test that `LoadCloudConfig` returns a `CloudRESTConfig` with token and stack info from the config:

```go
package providers_test

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "testing"

    "github.com/grafana/grafanactl/internal/cloud"
    "github.com/grafana/grafanactl/internal/providers"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestConfigLoader_LoadCloudConfig(t *testing.T) {
    // Start a mock GCOM server.
    gcomServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        assert.Contains(t, r.URL.Path, "/api/instances/teststack")
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(cloud.StackInfo{
            ID:                        123,
            Slug:                      "teststack",
            AgentManagementInstanceID:  456,
            AgentManagementInstanceURL: "https://fleet.example.com",
        })
    }))
    defer gcomServer.Close()

    // Write a temp config file with cloud section.
    configYAML := fmt.Sprintf(`
current-context: test
contexts:
  test:
    grafana:
      server: https://teststack.grafana.net
      token: grafana-sa-token
    cloud:
      token: cloud-access-token
      api-url: %s
`, gcomServer.URL)

    dir := t.TempDir()
    configPath := filepath.Join(dir, "config.yaml")
    require.NoError(t, os.WriteFile(configPath, []byte(configYAML), 0o600))

    loader := &providers.ConfigLoader{}
    // TODO: set configFile on loader (needs accessor or constructor)
    cfg, err := loader.LoadCloudConfig(context.Background())

    require.NoError(t, err)
    assert.Equal(t, "cloud-access-token", cfg.Token)
    assert.Equal(t, 123, cfg.Stack.ID)
    assert.Equal(t, "https://fleet.example.com", cfg.Stack.AgentManagementInstanceURL)
}
```

Note: This test may need adjustment based on how the ConfigLoader is initialized with a config file path. The exact wiring depends on the existing test patterns — check `internal/providers/configloader.go` for how `configFile` is set.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/providers/ -run TestConfigLoader_LoadCloudConfig -v`
Expected: FAIL — `LoadCloudConfig` does not exist

**Step 3: Add CloudRESTConfig type and LoadCloudConfig**

Add to `internal/providers/configloader.go`:

```go
// CloudRESTConfig holds Grafana Cloud credentials and discovered stack info.
// Providers like fleet, oncall, k6 use this instead of NamespacedRESTConfig.
type CloudRESTConfig struct {
    // Token is the Grafana Cloud access policy token.
    Token string

    // Stack is the discovered GCOM stack info.
    Stack cloud.StackInfo

    // Namespace is the K8s-style namespace for resource envelopes.
    Namespace string
}

// LoadCloudConfig loads Grafana Cloud credentials from config, discovers
// stack info via GCOM, and returns a CloudRESTConfig.
// The cloud token and stack slug are resolved from:
//  1. GRAFANA_CLOUD_TOKEN / GRAFANA_CLOUD_STACK / GRAFANA_CLOUD_API_URL env vars
//  2. Config file: cloud.token / cloud.stack / cloud.api-url
//  3. Stack slug derived from grafana.server URL if not set explicitly
func (l *ConfigLoader) LoadCloudConfig(ctx context.Context) (CloudRESTConfig, error) {
    loaded, err := l.loadConfigForCloud(ctx)
    if err != nil {
        return CloudRESTConfig{}, err
    }

    curCtx := loaded.GetCurrentContext()

    if curCtx.Cloud == nil || curCtx.Cloud.Token == "" {
        return CloudRESTConfig{}, errors.New(
            "cloud token not configured: set cloud.token in config or GRAFANA_CLOUD_TOKEN env var")
    }

    slug := curCtx.ResolveStackSlug()
    if slug == "" {
        return CloudRESTConfig{}, errors.New(
            "cloud stack not configured: set cloud.stack in config, GRAFANA_CLOUD_STACK env var, " +
            "or use a *.grafana.net server URL")
    }

    gcomURL := curCtx.ResolveGCOMURL()
    gcomClient := cloud.NewGCOMClient(gcomURL, curCtx.Cloud.Token)
    stackInfo, err := gcomClient.GetStack(ctx, slug)
    if err != nil {
        return CloudRESTConfig{}, fmt.Errorf("failed to discover cloud stack %q: %w", slug, err)
    }

    // Derive namespace same as Grafana config does.
    namespace := "default"
    if curCtx.Grafana != nil && !curCtx.Grafana.IsEmpty() {
        restCfg := curCtx.ToRESTConfig(ctx)
        namespace = restCfg.Namespace
    }

    return CloudRESTConfig{
        Token:     curCtx.Cloud.Token,
        Stack:     *stackInfo,
        Namespace: namespace,
    }, nil
}
```

The `loadConfigForCloud` method is similar to the existing `LoadRESTConfig` internals but with relaxed validation (cloud config doesn't require `grafana.server` to be set):

```go
func (l *ConfigLoader) loadConfigForCloud(ctx context.Context) (config.Config, error) {
    source := l.configSource()

    overrides := []config.Override{
        // Apply env vars into the current context.
        func(cfg *config.Config) error {
            // ... same env override logic as LoadGrafanaConfig ...
            // Plus: parse GRAFANA_CLOUD_* env vars into cloud config
            return nil
        },
    }

    // Context name resolution (same as existing).
    ctxName := l.ctxName
    if ctxName == "" {
        ctxName = config.ContextNameFromCtx(ctx)
    }
    if ctxName != "" {
        overrides = append(overrides, func(cfg *config.Config) error {
            if !cfg.HasContext(ctxName) {
                return config.ContextNotFound(ctxName)
            }
            cfg.CurrentContext = ctxName
            return nil
        })
    }

    // Relaxed validation — only check context exists, don't require grafana config.
    overrides = append(overrides, func(cfg *config.Config) error {
        if !cfg.HasContext(cfg.CurrentContext) {
            return config.ContextNotFound(cfg.CurrentContext)
        }
        return nil
    })

    return config.Load(ctx, source, overrides...)
}
```

**Step 4: Run tests**

Run: `go test ./internal/providers/ -run TestConfigLoader_LoadCloudConfig -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/providers/configloader.go internal/providers/configloader_test.go
git commit -m "feat(providers): add LoadCloudConfig for Grafana Cloud auth"
```

---

### Task 4: Rename LoadRESTConfig → LoadGrafanaConfig

**Files:**
- Modify: `internal/providers/configloader.go` (rename method)
- Modify: 41 files that reference `LoadRESTConfig` (see grep results)

**Step 1: Rename the method**

In `internal/providers/configloader.go`, rename:
```go
func (l *ConfigLoader) LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error) {
```

**Step 2: Update all callers**

Use LSP rename or find-and-replace across all files that call `LoadRESTConfig`:

Key callers to update:
- `internal/providers/incidents/resource_adapter.go` — `RESTConfigLoader` interface
- `internal/providers/incidents/commands.go` — all command functions
- `internal/providers/slo/definitions/*.go` — SLO commands
- `internal/providers/slo/reports/*.go` — reports commands
- `internal/providers/alert/*.go` — alert commands
- `internal/providers/synth/provider.go` — synth config loader
- `cmd/grafanactl/resources/*.go` — resource commands
- `cmd/grafanactl/datasources/**/*.go` — datasource commands
- `cmd/grafanactl/dashboards/snapshot.go`
- `cmd/grafanactl/dev/import.go`
- `cmd/grafanactl/api/command.go`
- `cmd/grafanactl/config/command.go`

Also rename the `RESTConfigLoader` interface in incidents:
```go
type GrafanaConfigLoader interface {
    LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error)
}
```

**Step 3: Run full build + tests**

Run: `go build ./... && go test ./... -count=1`
Expected: PASS — no compilation errors, all tests pass

**Step 4: Commit**

```bash
git add -A
git commit -m "refactor(providers): rename LoadRESTConfig → LoadGrafanaConfig"
```

---

### Task 5: Refactor fleet provider to use LoadCloudConfig

**Files:**
- Modify: `internal/providers/fleet/provider.go` (remove per-provider config, use cloud config)
- Modify: `internal/providers/fleet/client_test.go` (if needed)
- Modify: `internal/providers/fleet/provider_test.go` (if needed)

**Step 1: Remove fleet-specific ConfigKeys**

In `provider.go`, change `ConfigKeys()` to return nil — fleet no longer needs per-provider config keys:

```go
func (p *FleetProvider) ConfigKeys() []providers.ConfigKey {
    return nil
}
```

**Step 2: Replace configLoader.LoadFleetConfig with ConfigLoader.LoadCloudConfig**

The `fleetHelper.withClient` method changes from:
```go
// Before: manual fleet config
url, instanceID, token, _, err := h.loader.LoadFleetConfig(ctx)
client := NewClient(url, instanceID, token, instanceID != "")
```

To:
```go
// After: cloud config with GCOM discovery
cloudCfg, err := h.loader.LoadCloudConfig(ctx)
stack := cloudCfg.Stack
client := NewClient(
    stack.AgentManagementInstanceURL,
    fmt.Sprintf("%d", stack.AgentManagementInstanceID),
    cloudCfg.Token,
    true, // always basic auth for fleet
)
```

**Step 3: Remove LoadFleetConfig and fleet-specific env vars**

Delete the entire `LoadFleetConfig` method, the `FleetConfigLoader` interface, and related code.

**Step 4: Update resource adapter factories**

The `NewPipelineAdapterFactory` and `NewCollectorAdapterFactory` change their loader interface:

```go
// Before:
type FleetConfigLoader interface {
    LoadFleetConfig(ctx context.Context) (url, instanceID, token, namespace string, err error)
}

// After: use providers.ConfigLoader directly
type cloudConfigLoader interface {
    LoadCloudConfig(ctx context.Context) (providers.CloudRESTConfig, error)
}
```

**Step 5: Update init() registration**

The `init()` function uses a `providers.ConfigLoader` instead of the fleet-specific loader:

```go
func init() {
    providers.Register(&FleetProvider{})

    loader := &providers.ConfigLoader{}
    adapter.Register(adapter.Registration{
        Factory:    NewPipelineAdapterFactory(loader),
        // ...
    })
    adapter.Register(adapter.Registration{
        Factory:    NewCollectorAdapterFactory(loader),
        // ...
    })
}
```

**Step 6: Run full verification**

Run: `GRAFANACTL_AGENT_MODE=false make all`
Expected: PASS — lint, tests, build, docs all green

**Step 7: Commit**

```bash
git add internal/providers/fleet/
git commit -m "refactor(fleet): use CloudConfig instead of per-provider auth"
```

---

## Config UX after implementation

```bash
# Set up cloud auth (one-time)
grafanactl config set contexts.mystack.cloud.token <access-policy-token>

# Optional: override stack slug (auto-derived from grafana.server URL)
grafanactl config set contexts.mystack.cloud.stack mystack

# Optional: use dev environment
grafanactl config set contexts.mystack.cloud.api-url grafana-dev.com

# Fleet commands now "just work" — no per-provider config
grafanactl fleet pipelines list
grafanactl fleet collectors list
```

## Env var support

```bash
GRAFANA_CLOUD_TOKEN=<token> grafanactl fleet pipelines list
GRAFANA_CLOUD_STACK=mystack grafanactl fleet pipelines list
GRAFANA_CLOUD_API_URL=grafana-dev.com grafanactl fleet pipelines list
```
