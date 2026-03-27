# Alert Provider Design

## Overview

Add a dedicated alert provider to gcx, following the SLO provider pattern.

## API

### Read Operations (Prometheus-compatible API)

**Base:** `/api/prometheus/grafana/api/v1/rules`

| Query Param | Description |
|-------------|-------------|
| `rule_uid` | Filter by specific rule UID |
| `rule_group` | Filter by alert group name |
| `folder_uid` | Filter by folder UID |
| `group_limit` | Limit number of groups returned |

Response is always in groups format:
```json
{
  "status": "success",
  "data": {
    "groups": [{
      "name": "GroupName",
      "folderUid": "...",
      "rules": [{ "uid": "...", "state": "inactive", ... }]
    }]
  }
}
```

### Write Operations (K8s API)

**Base:** `/apis/rules.alerting.grafana.app/v0alpha1/namespaces/{namespace}/alertrules`

Used only for push/delete operations.

## Command Structure

```
gcx alert
├── rules
│   ├── list [--group <name>] [--folder <uid>]
│   ├── get <uid>
│   ├── push FILE...
│   ├── pull [--group <name>]
│   ├── delete <uid>...
│   └── status [uid]
│
└── groups
    ├── list
    ├── get <name>
    └── status [name]
```

## Package Layout

```
internal/alert/
├── provider.go           # AlertProvider struct
├── client.go             # HTTP client for Prometheus API (shared)
├── types.go              # RulesResponse, RuleGroup, RuleStatus
├── rules/
│   ├── commands.go       # CRUD + status commands
│   ├── k8s_client.go     # K8s dynamic client (push/delete only)
│   └── adapter.go        # K8s resource ↔ file conversion
│
└── groups/
    └── commands.go       # list, get, status (uses shared client)
```

## Key Design Decisions

### Single HTTP Client for Reads

All read operations (list, get, status) use the Prometheus-compatible API:
- Built-in filtering via query params (`rule_uid`, `rule_group`, `folder_uid`)
- Groups included in response - no need to derive from labels
- Status data (state, health, lastEvaluation) included in same response

### K8s Client Only for Writes

The K8s dynamic client is only used for:
- `push` - Create/Update via K8s API
- `delete` - Delete via K8s API

### No Additional Config Keys

Uses Grafana SA token (same as SLO provider):

```go
func (p *AlertProvider) ConfigKeys() []providers.ConfigKey { return nil }
```

## References

- SLO provider: `internal/slo/provider.go`
- Provider interface: `internal/providers/provider.go`
- Registration: `cmd/gcx/root/command.go`
