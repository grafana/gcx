---
name: generate-resource-stubs
description: >
  Use when the user wants to create new Grafana dashboard or alert rule Go
  files from scratch, generate typed stubs, add a new dashboard to a project,
  or write grafana-foundation-sdk builder code. Triggers on "new dashboard",
  "generate dashboard", "add dashboard", "create alert rule", "generate stub",
  "foundation-sdk builder", "dashboard as code from scratch".
---

# Generate Typed Resource Stubs

Generate ready-to-edit Go files for dashboards and alert rules using
`gcx dev generate`. The generated code uses the grafana-foundation-sdk
builder pattern and compiles without modification.

## Quick Start

```bash
# Dashboard stub
gcx dev generate dashboards/my-service-overview.go

# Alert rule stub
gcx dev generate alerts/high-cpu-usage.go

# Batch generation
gcx dev generate dashboards/a.go dashboards/b.go alerts/c.go
```

## How Type Inference Works

The resource type is inferred from the **immediate parent directory**:

| Directory | Type |
|-----------|------|
| `dashboards/` or `dashboard/` | dashboard |
| `alerts/`, `alertrules/`, or `alertrule/` | alertrule |

Override with `--type` when the directory doesn't match:

```bash
gcx dev generate internal/monitoring/cpu-alert.go --type alertrule
```

## Output

- Filename is normalized to snake_case: `my-dashboard.go` → `my_dashboard.go`
- Function name is CamelCase: `my-dashboard` → `MyDashboard()`
- Package name is derived from the parent directory

## After Generation: Building Real Dashboards

The generated stub is a starting point. See `references/foundation-sdk-guide.md`
for the full builder API reference to customize your dashboards and alerts.

### Dashboard Customization Cheat Sheet

```go
import (
    dashboard "github.com/grafana/grafana-foundation-sdk/go/dashboardv2beta1"
    "github.com/grafana/grafana-foundation-sdk/go/prometheus"
    "github.com/grafana/grafana-foundation-sdk/go/timeseries"
)

builder := dashboard.NewDashboardBuilder("My Dashboard").
    Tags([]string{"team:platform", "env:production"}).
    Editable(true).
    // Add a panel
    Panel("requests-panel",
        dashboard.NewPanelBuilder().
            Title("Request Rate").
            Visualization(timeseries.NewVisualizationBuilder()).
            Data(
                dashboard.NewQueryGroupBuilder().
                    Target(
                        dashboard.NewTargetBuilder().Query(
                            prometheus.NewDataqueryBuilder().
                                Expr(`rate(http_requests_total[$__rate_interval])`).
                                LegendFormat("{{method}} {{status}}"),
                        ),
                    ),
            ),
    ).
    // Layout
    AutoGridLayout(
        dashboard.AutoGrid().
            WithItem("requests-panel"),
    )

return dashboard.Manifest("my-dashboard", builder)
```

### Alert Rule Customization Cheat Sheet

```go
import (
    "github.com/grafana/grafana-foundation-sdk/go/alerting"
    "github.com/grafana/grafana-foundation-sdk/go/resource"
)

rule := alerting.NewRuleBuilder("High CPU Usage").
    Condition("A").
    For("5m").
    FolderUID("my-folder").
    RuleGroup("cpu-alerts").
    Labels(map[string]string{
        "severity": "critical",
        "team":     "platform",
    }).
    Annotations(map[string]string{
        "summary":     "CPU usage above 90% for 5 minutes",
        "description": "Instance {{ $labels.instance }} has high CPU usage.",
    })
```

## Common Issues

| Issue | Fix |
|-------|-----|
| "cannot infer resource type" | Directory name doesn't match known types; use `--type dashboard` or `--type alertrule` |
| "file already exists" | Delete the existing file first or use a different name |
| Need to add to registry | Manually add the function call to your `All()` function in `all.go` |
