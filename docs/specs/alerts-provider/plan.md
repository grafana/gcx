# Alerts Provider Implementation Plan

## Overview

Add an alerts provider following the SLO provider pattern, exposing alert rules via `grafanactl alerts rules` commands.

## API

### Read Operations (Prometheus-compatible API)

**Base:** `/api/prometheus/grafana/api/v1/rules`

| Query Param | Description |
|-------------|-------------|
| `rule_uid` | Filter by specific rule UID |
| `rule_group` | Filter by alert group name |
| `folder_uid` | Filter by folder UID |
| `group_limit` | Limit number of groups returned |

Response is always in groups format, even for single rule queries.

### Write Operations (K8s API)

**Base:** `/apis/rules.alerting.grafana.app/v0alpha1/namespaces/{namespace}/alertrules`

Used only for push/delete operations where K8s API is required.

## Package Structure

```
internal/alert/
├── provider.go           # AlertProvider implementation
├── provider_test.go
├── client.go             # HTTP client for /api/prometheus/grafana/api/v1/rules (shared)
├── client_test.go
├── types.go              # Response types (RulesResponse, RuleGroup, RuleStatus)
├── rules/
│   ├── commands.go       # list, get, push, pull, delete, status commands
│   ├── k8s_client.go     # K8s dynamic client (push/delete only)
│   └── adapter.go        # K8s resource ↔ file conversion for push/pull
│
└── groups/
    └── commands.go       # list, get, status commands (uses shared client)
```

## Implementation Steps

### 1. Create provider skeleton

**File:** `internal/alert/provider.go`

```go
package alert

type AlertProvider struct{}

func (p *AlertProvider) Name() string      { return "alert" }
func (p *AlertProvider) ShortDesc() string { return "Manage Grafana alerting resources." }
func (p *AlertProvider) Commands() []*cobra.Command { ... }
func (p *AlertProvider) Validate(cfg map[string]string) error { return nil }
func (p *AlertProvider) ConfigKeys() []providers.ConfigKey { return nil }
```

### 2. Register provider

**File:** `cmd/grafanactl/root/command.go`

```go
import alertprovider "github.com/grafana/grafanactl/internal/alert"

func allProviders() []providers.Provider {
    return append(
        providers.All(),
        &sloprovider.SLOProvider{},
        &alertprovider.AlertProvider{},  // Add this
    )
}
```

### 3. Implement shared HTTP client (Prometheus-compatible API)

**File:** `internal/alert/client.go`

Single HTTP client for all read operations using the Prometheus-compatible rules API.

```go
package alert

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "net/url"

    "github.com/grafana/grafanactl/internal/config"
    "k8s.io/client-go/rest"
)

const basePath = "/api/prometheus/grafana/api/v1/rules"

// Client fetches alert rules and groups from the Prometheus-compatible API.
type Client struct {
    httpClient *http.Client
    host       string
}

// NewClient creates a new alert client.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
    httpClient, err := rest.HTTPClientFor(&cfg.Config)
    if err != nil {
        return nil, fmt.Errorf("failed to create HTTP client: %w", err)
    }
    return &Client{httpClient: httpClient, host: cfg.Host}, nil
}

// ListOptions configures filtering for List operations.
type ListOptions struct {
    RuleUID    string // Filter by specific rule UID
    GroupName  string // Filter by alert group name (rule_group param)
    FolderUID  string // Filter by folder UID
    GroupLimit int    // Limit number of groups returned
}

// List returns rules matching the given options.
// Response is always in groups format.
func (c *Client) List(ctx context.Context, opts ListOptions) (*RulesResponse, error) {
    params := url.Values{}
    if opts.RuleUID != "" {
        params.Set("rule_uid", opts.RuleUID)
    }
    if opts.GroupName != "" {
        params.Set("rule_group", opts.GroupName)
    }
    if opts.FolderUID != "" {
        params.Set("folder_uid", opts.FolderUID)
    }
    if opts.GroupLimit > 0 {
        params.Set("group_limit", fmt.Sprintf("%d", opts.GroupLimit))
    }

    path := basePath
    if len(params) > 0 {
        path += "?" + params.Encode()
    }

    return c.doRequest(ctx, path)
}

// GetRule returns a single rule by UID.
// Convenience method wrapping List with RuleUID filter.
func (c *Client) GetRule(ctx context.Context, uid string) (*RuleStatus, error) {
    resp, err := c.List(ctx, ListOptions{RuleUID: uid})
    if err != nil {
        return nil, err
    }

    // Extract single rule from groups response
    for _, group := range resp.Data.Groups {
        for _, rule := range group.Rules {
            if rule.UID == uid {
                return &rule, nil
            }
        }
    }
    return nil, fmt.Errorf("rule %s not found", uid)
}

// ListGroups returns all unique groups.
// Convenience method that extracts groups from List response.
func (c *Client) ListGroups(ctx context.Context) ([]RuleGroup, error) {
    resp, err := c.List(ctx, ListOptions{})
    if err != nil {
        return nil, err
    }
    return resp.Data.Groups, nil
}

// GetGroup returns a single group by name with all its rules.
func (c *Client) GetGroup(ctx context.Context, name string) (*RuleGroup, error) {
    resp, err := c.List(ctx, ListOptions{GroupName: name})
    if err != nil {
        return nil, err
    }

    for _, group := range resp.Data.Groups {
        if group.Name == name {
            return &group, nil
        }
    }
    return nil, fmt.Errorf("group %s not found", name)
}

func (c *Client) doRequest(ctx context.Context, path string) (*RulesResponse, error) {
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+path, nil)
    if err != nil {
        return nil, fmt.Errorf("failed to create request: %w", err)
    }

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, fmt.Errorf("failed to execute request: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("request failed with status %d", resp.StatusCode)
    }

    var result RulesResponse
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, fmt.Errorf("failed to decode response: %w", err)
    }

    return &result, nil
}
```

**Key points:**
- Single client for all read operations (rules + groups)
- `ListOptions` struct for flexible filtering via query params
- Convenience methods: `GetRule`, `ListGroups`, `GetGroup`
- Response always in groups format - methods extract relevant data

### 3b. Implement K8s client (write operations only)

**File:** `internal/alert/rules/k8s_client.go`

K8s dynamic client for push/delete operations only.

```go
package rules

import (
    "context"

    "github.com/grafana/grafanactl/internal/config"
    "github.com/grafana/grafanactl/internal/resources"
    "github.com/grafana/grafanactl/internal/resources/dynamic"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
    "k8s.io/apimachinery/pkg/runtime/schema"
)

const (
    Group    = "rules.alerting.grafana.app"
    Version  = "v0alpha1"
    Resource = "alertrules"
    Kind     = "AlertRule"
)

// Descriptor returns the K8s resource descriptor for alert rules.
func Descriptor() resources.Descriptor {
    return resources.Descriptor{
        GroupVersion: schema.GroupVersion{Group: Group, Version: Version},
        Kind:         Kind,
        Singular:     "alertrule",
        Plural:       Resource,
    }
}

// K8sClient wraps the dynamic client for write operations.
type K8sClient struct {
    client *dynamic.NamespacedClient
    desc   resources.Descriptor
}

// NewK8sClient creates a new K8s client for alert rules.
func NewK8sClient(cfg config.NamespacedRESTConfig) (*K8sClient, error) {
    client, err := dynamic.NewDefaultNamespacedClient(cfg)
    if err != nil {
        return nil, err
    }
    return &K8sClient{client: client, desc: Descriptor()}, nil
}

// Create creates a new alert rule.
func (c *K8sClient) Create(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
    return c.client.Create(ctx, c.desc, obj, metav1.CreateOptions{})
}

// Update updates an existing alert rule.
func (c *K8sClient) Update(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
    return c.client.Update(ctx, c.desc, obj, metav1.UpdateOptions{})
}

// Delete deletes an alert rule by UID.
func (c *K8sClient) Delete(ctx context.Context, uid string) error {
    return c.client.Delete(ctx, c.desc, uid, metav1.DeleteOptions{})
}

// Get fetches a rule for update (to get resourceVersion).
func (c *K8sClient) Get(ctx context.Context, uid string) (*unstructured.Unstructured, error) {
    return c.client.Get(ctx, c.desc, uid, metav1.GetOptions{})
}
```

**Key points:**
- Only used for push (create/update) and delete operations
- No List - use the HTTP client for reads
- Get is only for fetching resourceVersion before update

### 4. Implement commands

**File:** `internal/alert/rules/commands.go`

Follow SLO definitions pattern:
- `list` - table output with NAME, FOLDER, STATE columns
- `get <name>` - YAML output
- `push FILE...` - upsert from files
- `pull` - write to disk
- `delete <name>...` - with confirmation
- `status [name]` - detailed status (evaluation history, last eval time, errors)

### 5. Define types

**File:** `internal/alert/types.go`

Response types for the Prometheus-compatible API.

```go
package alert

// RulesResponse is the response from /api/prometheus/grafana/api/v1/rules.
type RulesResponse struct {
    Status string    `json:"status"`
    Data   RulesData `json:"data"`
}

type RulesData struct {
    Groups []RuleGroup       `json:"groups"`
    Totals map[string]int    `json:"totals,omitempty"`
}

type RuleGroup struct {
    Name           string            `json:"name"`
    File           string            `json:"file"`           // Display path like "» Users/@username"
    FolderUID      string            `json:"folderUid"`
    Rules          []RuleStatus      `json:"rules"`
    Totals         map[string]int    `json:"totals,omitempty"`
    Interval       int               `json:"interval"`
    LastEvaluation string            `json:"lastEvaluation"`
    EvaluationTime float64           `json:"evaluationTime"`
}

type RuleStatus struct {
    State                 string                 `json:"state"`      // inactive, pending, firing
    Name                  string                 `json:"name"`
    UID                   string                 `json:"uid"`
    FolderUID             string                 `json:"folderUid"`
    Health                string                 `json:"health"`     // ok, error
    Type                  string                 `json:"type"`       // alerting
    Query                 string                 `json:"query"`
    LastEvaluation        string                 `json:"lastEvaluation"`
    EvaluationTime        float64                `json:"evaluationTime"`
    IsPaused              bool                   `json:"isPaused"`
    Labels                map[string]string      `json:"labels"`
    Annotations           map[string]string      `json:"annotations"`
    QueriedDatasourceUIDs []string               `json:"queriedDatasourceUIDs,omitempty"`
    NotificationSettings  *NotificationSettings  `json:"notificationSettings,omitempty"`
}

type NotificationSettings struct {
    Receiver      string `json:"receiver,omitempty"`
    GroupInterval string `json:"group_interval,omitempty"`
}
```

### 6. Implement status command

**File:** `internal/alert/rules/status.go`

Following the SLO status pattern:
1. Fetch alert rule(s) from K8s API
2. Fetch status data (source TBD - may come from API response or Prometheus)
3. Merge into StatusResult
4. Output as table/wide/json

**Potential status fields:**
- State: firing, pending, normal/inactive
- Last evaluation time
- Evaluation duration
- Error message (if evaluation failed)
- Active alerts count

**Status data source:** Prometheus-compatible rules API

- **List all:** `GET /api/prometheus/grafana/api/v1/rules`
- **Single rule:** `GET /api/prometheus/grafana/api/v1/rules?rule_uid={name}`

**Response structure:**
```json
{
  "status": "success",
  "data": {
    "groups": [{
      "name": "GroupName",
      "file": "» Users/@username",
      "folderUid": "...",
      "rules": [{
        "state": "inactive|pending|firing",
        "name": "Rule Name",
        "uid": "rule-uid",
        "folderUid": "...",
        "health": "ok|error",
        "lastEvaluation": "2024-01-01T00:00:00Z",
        "evaluationTime": 0.001,
        "isPaused": false,
        "query": "...",
        "labels": {...},
        "annotations": {...}
      }]
    }]
  }
}
```

**Key status fields for display:**
- `state`: inactive, pending, firing
- `health`: ok, error
- `lastEvaluation`: timestamp
- `evaluationTime`: duration in seconds
- `isPaused`: boolean

**Status table output:**
```
NAME                    UID           STATE     HEALTH  LAST_EVAL     PAUSED
Fayzal Enrichment Test  cetfmyb4...   inactive  ok      2024-01-01    yes
High CPU Alert          abc123...     firing    ok      5m ago        no
```

**Wide format (-o wide):** adds FOLDER, EVAL_TIME, QUERY columns

## Command Structure

```
grafanactl alert
├── rules
│   ├── list [--group <name>]  # Table with NAME, GROUP, FOLDER, STATE columns
│   ├── get <uid>              # YAML/JSON output
│   ├── push FILE...           # Upsert from files
│   ├── pull [--group <name>]  # Write to disk (optionally filter by group)
│   ├── delete <uid>...        # With confirmation
│   └── status [uid]           # Detailed status (state, health, last eval)
│
└── groups
    ├── list                   # Table with NAME, FOLDER, RULES_COUNT columns
    ├── get <name>             # List rules in group (YAML/JSON)
    └── status [name]          # Status of all rules in the group
```

**Note:** Alert rules reference their group via `metadata.labels."grafana.com/group"`

### Groups Implementation

Groups come directly from the Prometheus API - no need to derive from labels.

**File:** `internal/alert/groups/commands.go`

```go
package groups

import (
    "github.com/grafana/grafanactl/internal/alert"
)

// listCommand: Get all groups directly from API
func newListCommand(client *alert.Client) *cobra.Command {
    // ...
    RunE: func(cmd *cobra.Command, args []string) error {
        groups, err := client.ListGroups(ctx)  // Returns []RuleGroup directly
        if err != nil {
            return err
        }
        // Output as table: NAME, FOLDER, RULES_COUNT, INTERVAL
        return codec.Encode(cmd.OutOrStdout(), groups)
    }
}

// getCommand: Get single group with all its rules
func newGetCommand(client *alert.Client) *cobra.Command {
    // ...
    RunE: func(cmd *cobra.Command, args []string) error {
        group, err := client.GetGroup(ctx, args[0])  // Uses ?rule_group=<name>
        if err != nil {
            return err
        }
        // Output as YAML/JSON - includes group metadata + all rules
        return codec.Encode(cmd.OutOrStdout(), group)
    }
}

// statusCommand: Status for all rules in a group (same as get, different output)
func newStatusCommand(client *alert.Client) *cobra.Command {
    // ...
    RunE: func(cmd *cobra.Command, args []string) error {
        var groups []alert.RuleGroup
        if len(args) > 0 {
            group, err := client.GetGroup(ctx, args[0])
            if err != nil {
                return err
            }
            groups = []alert.RuleGroup{*group}
        } else {
            groups, err = client.ListGroups(ctx)
            if err != nil {
                return err
            }
        }
        // Output as status table showing rules with STATE, HEALTH, etc.
        return codec.Encode(cmd.OutOrStdout(), groups)
    }
}
```

**Key points:**
- Uses shared `alert.Client` - no separate client needed
- `ListGroups()` returns groups directly from API response
- `GetGroup(name)` uses `?rule_group=<name>` query param
- Much simpler than K8s label selector approach

## Key Differences from SLO Provider

| Aspect | SLO Provider | Alerts Provider |
|--------|--------------|-----------------|
| API Type | Plugin API (`/api/plugins/...`) | K8s API (`/apis/...`) |
| Client | Custom HTTP client | K8s dynamic client |
| Resource ID | `uuid` | `metadata.name` |

## Files to Create

### Provider + Shared Client
1. `internal/alert/provider.go`
2. `internal/alert/provider_test.go`
3. `internal/alert/client.go` - HTTP client for Prometheus API (shared)
4. `internal/alert/client_test.go`
5. `internal/alert/types.go` - Response types (RulesResponse, RuleGroup, RuleStatus)

### Rules
6. `internal/alert/rules/commands.go` - list, get, push, pull, delete, status
7. `internal/alert/rules/k8s_client.go` - K8s dynamic client (push/delete only)
8. `internal/alert/rules/adapter.go` - K8s resource ↔ file conversion

### Groups
9. `internal/alert/groups/commands.go` - list, get, status (uses shared client)

## Files to Modify

1. `cmd/grafanactl/root/command.go` - register provider

## Verification

```bash
make lint && make tests && make build && go build -o bin/grafanactl ./cmd/grafanactl

.bin/grafanactl providers                    # Should list "alert"

# Rules commands
.bin/grafanactl alert rules list             # List all alert rules
.bin/grafanactl alert rules list --group <name>  # Filter by group
.bin/grafanactl alert rules get <uid>        # Get single rule as YAML
.bin/grafanactl alert rules pull -d ./alerts/
.bin/grafanactl alert rules push ./alerts/*.yaml
.bin/grafanactl alert rules delete <uid> -f
.bin/grafanactl alert rules status           # Status of all rules
.bin/grafanactl alert rules status <uid>     # Status of single rule
.bin/grafanactl alert rules status -o wide   # Extended status columns

# Groups commands (derived from rules)
.bin/grafanactl alert groups list            # List all groups (unique labels)
.bin/grafanactl alert groups get <name>      # List rules in group
.bin/grafanactl alert groups status          # Status summary per group
.bin/grafanactl alert groups status <name>   # Status of rules in group
```
