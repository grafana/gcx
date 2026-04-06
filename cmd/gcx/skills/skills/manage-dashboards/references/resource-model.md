# Grafana Resource Model

This document describes how Grafana resources are structured and how they relate to each other.

## Resource Structure

All Grafana resources follow Kubernetes-style conventions:

```yaml
apiVersion: dashboard.grafana.app/v1alpha1
kind: Dashboard
metadata:
  name: my-dashboard
  namespace: default  # org-id (on-prem) or stack-id (cloud)
  uid: abc123
  annotations:
    grafana.app/managed-by: gcx
    grafana.app/source-file: /path/to/dashboard.yaml
    grafana.app/source-format: yaml
spec:
  # Dashboard specification
  title: "My Dashboard"
  panels: [...]
```

### Key Fields

- **apiVersion**: Format is `{resource}.{group}/{version}` (e.g., `dashboard.grafana.app/v1alpha1`)
- **kind**: Resource type (Dashboard, Folder, Datasource, etc.)
- **metadata.name**: Human-readable name
- **metadata.uid**: Unique identifier (required for updates, auto-generated for creates)
- **metadata.namespace**: Organization ID (on-prem) or Stack ID (Grafana Cloud)
- **metadata.annotations**: Metadata about management and source
- **spec**: Resource-specific configuration

## Resource Relationships

### Hierarchical Dependencies

```
Grafana Instance
├── Organizations (on-prem) / Stacks (cloud)
    ├── Folders
    │   └── Dashboards (must reference parent folder)
    ├── Datasources
    ├── Alert Rules
    ├── Contact Points
    └── Notification Policies
```

### Dependency Rules

1. **Folders → Dashboards**: Dashboards can optionally belong to a folder
   - Dashboard `spec.folderUID` must reference an existing folder UID
   - gcx pushes folders before dashboards to ensure dependencies exist
   - Dashboards without folderUID go to the "General" folder

2. **Datasources → Dashboards**: Dashboard panels reference datasources by UID
   - Panel `datasource.uid` must match an existing datasource UID
   - Datasources should exist before pushing dashboards that reference them

3. **Alert Rules → Datasources**: Alert rules query datasources
   - Alert rule queries reference datasource UIDs
   - Datasources must exist before creating alerts

4. **Alert Rules → Folders**: Alert rules can be organized in folders
   - Similar to dashboards, optional folder relationship

## Resource Groups

Grafana organizes resources into API groups:

### Core Resources
- **dashboard.grafana.app**: Dashboards, folders
- **datasource.grafana.app**: Datasources (generic)
- **prometheus.datasource.grafana.app**: Prometheus-specific operations
- **loki.datasource.grafana.app**: Loki-specific operations

### Alerting Resources
- **alerting.grafana.app**: Alert rules, contact points, notification policies
- **notifications.grafana.app**: Notification templates

### Access Control
- **iam.grafana.app**: Service accounts, API keys (read-only in gcx)
- **team.grafana.app**: Teams and permissions

### Excluded Groups
gcx excludes certain groups from normal operations:
- **featuretoggle.grafana.app**: Internal feature flags
- **iam.grafana.app**: Sensitive access control (requires special handling)

## Manager Metadata

Resources track which tool manages them:

```yaml
metadata:
  annotations:
    grafana.app/managed-by: gcx
    grafana.app/source-file: ./resources/dashboards/my-dashboard.yaml
    grafana.app/source-format: yaml
```

### Manager Behavior

- **Resources created by gcx**: Can be freely modified by gcx
- **Resources created by UI/Terraform/other**: Protected by default
  - Use `--include-managed` to modify (use with caution)
  - Prevents accidental overwrites from different tools

### Three-Way Merge (Future)

Currently gcx uses simple upsert logic. Future versions will implement proper three-way merge:
- Track field ownership by manager
- Allow multiple managers to coexist
- Detect and resolve conflicts
- Similar to `kubectl apply` behavior

## Resource Versioning

### API Versions

Resources can have multiple API versions:
- **v1alpha1**: Alpha version, subject to breaking changes
- **v1beta1**: Beta version, more stable
- **v1**: Stable version (when available)

gcx uses **preferred version** by default (typically latest stable version).

### Resource Versions (Future)

Currently gcx does not track `resourceVersion` for optimistic locking. Future versions will:
- Include `resourceVersion` in metadata
- Detect concurrent modifications
- Retry on conflict with exponential backoff

## Discovery System

gcx dynamically discovers available resources using Grafana's API:

```bash
# Discover what's available
gcx resources schemas
```

Discovery process:
1. Calls Grafana's `ServerGroupsAndResources` API
2. Builds index of available resource types by GVK (Group, Version, Kind)
3. Determines preferred version for each resource type
4. Filters out excluded groups (featuretoggle, iam, etc.)
5. Caches results for subsequent operations

## Push Order

When pushing multiple resources, gcx ensures correct order:

1. **Phase 1 - Folders**: Create folders first
2. **Phase 2 - Other Resources**: Create dashboards, datasources, etc.
3. **Concurrent Operations**: Resources within same phase pushed concurrently (default 10 concurrent)

Example:
```bash
# This automatically handles ordering
gcx resources push dashboards folders

# Internally:
# 1. Pushes all folders first
# 2. Then pushes dashboards (which may reference folders)
```

## Source Tracking

gcx tracks where resources came from:

```yaml
metadata:
  annotations:
    grafana.app/source-file: ./resources/dashboards/my-dashboard.yaml
    grafana.app/source-format: yaml
```

Benefits:
- Round-trip preservation: Pull/push maintains original format
- Error context: Error messages include file path
- Debugging: Know which file caused an issue

## Resource Filtering

gcx supports flexible resource selection:

### By Kind
```bash
gcx resources pull dashboards
gcx resources pull dashboards folders
```

### By UID
```bash
gcx resources pull dashboards/my-dashboard-uid
gcx resources pull dashboards/uid1,uid2,uid3
```

### By Version
```bash
# Preferred version (default)
gcx resources pull dashboards

# All versions
gcx resources pull dashboards --all-versions

# Specific version
gcx resources pull dashboards.v1alpha1.dashboard.grafana.app
```

## Memory Considerations

gcx loads all resources into memory during operations:
- **Typical usage**: ~1MB per 100 dashboards
- **Practical limit**: ~10,000 resources before memory pressure
- **Mitigation**: Use selective pulling (specific resource types or UIDs)

Future versions may add streaming support for very large deployments.

## Resource Lifecycle

### Create
```bash
# Create new resource from file
gcx resources push -p ./my-dashboard.yaml
```
- UID auto-generated if not specified
- Manager metadata added automatically
- Folders created before dashboards

### Read
```bash
# Get resource from Grafana
gcx resources pull dashboards/my-dashboard-uid
```
- Fetches current state from Grafana
- Includes all metadata
- Format preserved if previously pulled

### Update
```bash
# Modify and push back
gcx resources edit dashboards/my-dashboard-uid
gcx resources push
```
- Requires existing UID in metadata
- Only gcx-managed resources (unless `--include-managed`)
- Server-managed fields stripped before update

### Delete
```bash
# Remove from Grafana
gcx resources delete dashboards/my-dashboard-uid
```
- Permanent deletion from Grafana
- Does not delete local files
- No dependency checking (dashboards not deleted when folder deleted)

## Best Practices

1. **Use UIDs consistently**: Always reference resources by UID, not name
2. **Respect manager boundaries**: Don't mix gcx with UI/Terraform for same resources
3. **Folders before dashboards**: Always push folders before dashboards that reference them
4. **Selective pulling**: Pull only what you need to reduce memory usage
5. **Version control**: Commit resources to git for history and collaboration
6. **Dry-run first**: Use `--dry-run` before production pushes
7. **Context per environment**: Create separate context for dev/staging/prod
8. **Source format**: Choose JSON or YAML and stick with it for consistency
