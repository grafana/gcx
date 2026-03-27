---
name: import-dashboards
description: >
  Use when the user wants to import existing Grafana dashboards as Go code,
  convert live dashboards to builder code, migrate dashboards to code,
  or reverse-engineer a dashboard from a Grafana instance. Triggers on
  "import dashboard", "convert to code", "dashboard as code", "export dashboard".
---

# Import Dashboards as Code

Import existing dashboards from a Grafana instance and convert them to Go
builder code using `gcx dev import`.

## Prerequisites

1. gcx installed and configured with a Grafana connection
   (`gcx config check` should show a valid context)
2. The target Grafana instance must be reachable

## Import Commands

### Import All Dashboards

```bash
gcx dev import dashboards
```

Writes Go files to `imported/` (default). Each dashboard becomes a function
returning `*resource.ManifestBuilder`.

### Import a Specific Dashboard

```bash
gcx dev import dashboards/my-dashboard-uid
```

### Import to a Custom Directory

```bash
gcx dev import dashboards --path src/grafana
```

### Import Multiple Resource Types

```bash
gcx dev import dashboards folders
```

## How It Works

```
Grafana Instance
    │
    ▼
gcx dev import dashboards/foo
    │
    ├── 1. Fetches resource via K8s API (/apis endpoint)
    ├── 2. Detects API version (v0alpha1, v1beta1, v2beta1)
    ├── 3. Runs version-specific converter (JSON → SDK builder code)
    ├── 4. Wraps in resource.NewManifestBuilder()
    └── 5. Writes to imported/<snake_case_name>.go
```

The converter produces Go code using the grafana-foundation-sdk builder
pattern — the same pattern used by `dev generate` and `dev scaffold`.

## Output Format

Each imported file looks like:

```go
package dashboards

import (
    "github.com/grafana/grafana-foundation-sdk/go/dashboardv2beta1"
    "github.com/grafana/grafana-foundation-sdk/go/resource"
)

func MyDashboard() *resource.ManifestBuilder {
    // ... builder code generated from the live dashboard
}
```

## Workflow: Import → Edit → Push

```bash
# 1. Import from Grafana
gcx dev import dashboards --path internal/dashboards

# 2. Review and edit the generated code
#    (fix up builder calls, add variables, etc.)

# 3. Push back to Grafana
gcx resources push
```

## Common Issues

| Issue | Fix |
|-------|-----|
| "no converter found" | Dashboard uses an unsupported API version; only v0alpha1, v1beta1, v2beta1 supported |
| Auth error (401/403) | Run `gcx config check` to verify credentials |
| Empty import | The selector matched no resources; check UID with `gcx resources get dashboards` |
| Imported code doesn't compile | Some complex dashboards produce converter output that needs manual fixup |
