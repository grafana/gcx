# Grafana Foundation SDK — Go Builder Reference (v0.0.12)

Reference for building dashboards and alerts using `grafana-foundation-sdk/go`.

## Import Paths

```go
import (
    dashboard "github.com/grafana/grafana-foundation-sdk/go/dashboardv2beta1"
    "github.com/grafana/grafana-foundation-sdk/go/resource"
    "github.com/grafana/grafana-foundation-sdk/go/alerting"
    // Visualization types
    "github.com/grafana/grafana-foundation-sdk/go/timeseries"
    "github.com/grafana/grafana-foundation-sdk/go/stat"
    "github.com/grafana/grafana-foundation-sdk/go/gauge"
    "github.com/grafana/grafana-foundation-sdk/go/barchart"
    "github.com/grafana/grafana-foundation-sdk/go/table"
    "github.com/grafana/grafana-foundation-sdk/go/heatmap"
    "github.com/grafana/grafana-foundation-sdk/go/piechart"
    "github.com/grafana/grafana-foundation-sdk/go/logs"
    "github.com/grafana/grafana-foundation-sdk/go/text"
    // Datasource query types
    "github.com/grafana/grafana-foundation-sdk/go/prometheus"
    "github.com/grafana/grafana-foundation-sdk/go/loki"
    "github.com/grafana/grafana-foundation-sdk/go/elasticsearch"
    "github.com/grafana/grafana-foundation-sdk/go/cloudwatch"
    "github.com/grafana/grafana-foundation-sdk/go/tempo"
    "github.com/grafana/grafana-foundation-sdk/go/testdata"
    // Utilities
    "github.com/grafana/grafana-foundation-sdk/go/common"
    "github.com/grafana/grafana-foundation-sdk/go/units"
)
```

## DashboardBuilder (dashboardv2beta1)

```go
dashboard.NewDashboardBuilder(title string) *DashboardBuilder
```

### Key Methods

| Method | Description |
|--------|-------------|
| `.Title(s)` | Dashboard title |
| `.Description(s)` | Dashboard description |
| `.Tags([]string{...})` | Tags for filtering |
| `.Editable(bool)` | Allow editing in UI |
| `.CursorSync(dashboard.DashboardCursorSync)` | Crosshair sync mode |
| `.LiveNow(bool)` | Stream live data |
| `.Revision(uint16)` | Dashboard revision |

### Adding Panels

```go
.Panel("panel-key", panelBuilder)       // Named panel
.LibraryPanel("key", libraryBuilder)    // Library panel reference
```

### Panel Builder

```go
dashboard.NewPanelBuilder().
    Title("Panel Title").
    Description("Panel description").
    Visualization(timeseries.NewVisualizationBuilder()).  // or stat, gauge, etc.
    Data(queryGroupBuilder)
```

### Query Group (data for a panel)

```go
dashboard.NewQueryGroupBuilder().
    Target(
        dashboard.NewTargetBuilder().Query(
            prometheus.NewDataqueryBuilder().
                Expr(`rate(http_requests_total[$__rate_interval])`).
                LegendFormat("{{method}}"),
        ),
    ).
    Target(  // Multiple targets
        dashboard.NewTargetBuilder().Query(
            prometheus.NewDataqueryBuilder().
                Expr(`rate(http_errors_total[$__rate_interval])`),
        ),
    )
```

### Layout Options

```go
// AutoGrid — simplest, auto-arranges panels
.AutoGridLayout(
    dashboard.AutoGrid().
        WithItem("panel-1").
        WithItem("panel-2"),
)

// Grid — explicit row/col positioning
.GridLayout(
    dashboard.NewGridLayoutBuilder().
        Row(dashboard.NewGridLayoutRowBuilder().
            Panel(0, 0, 12, 8, dashboard.NewGridLayoutItemBuilder().Element("panel-1")).
            Panel(12, 0, 12, 8, dashboard.NewGridLayoutItemBuilder().Element("panel-2")),
        ),
)

// Rows — group panels into collapsible rows
.RowsLayout(
    dashboard.NewRowsLayoutBuilder().
        Row(dashboard.NewRowsLayoutRowBuilder().
            Title("Row Title").
            Panel(dashboard.NewRowsLayoutPanelBuilder().Element("panel-1")),
        ),
)
```

### Variables

```go
.QueryVariable(
    dashboard.NewQueryVariableKindBuilder().
        Name("datasource").
        Label("Data Source").
        DatasourceType("prometheus"),
)
.IntervalVariable(
    dashboard.NewIntervalVariableKindBuilder().
        Name("interval").
        Values([]string{"1m", "5m", "15m", "1h"}),
)
```

### Wrapping for gcx

```go
return dashboard.Manifest("resource-name", builder)
```

## Visualization Types

### Timeseries
```go
timeseries.NewVisualizationBuilder()  // Line/area/bar chart over time
```

### Stat
```go
stat.NewVisualizationBuilder()  // Single big number
```

### Gauge
```go
gauge.NewVisualizationBuilder()  // Gauge with thresholds
```

### Table
```go
table.NewVisualizationBuilder()  // Data table
```

### Bar Chart
```go
barchart.NewVisualizationBuilder()  // Category bar chart
```

### Heatmap
```go
heatmap.NewVisualizationBuilder()  // Heatmap
```

### Logs
```go
logs.NewVisualizationBuilder()  // Log viewer
```

### Text
```go
text.NewVisualizationBuilder()  // Markdown/HTML text panel
```

## Datasource Query Types

### Prometheus
```go
prometheus.NewDataqueryBuilder().
    Expr(`rate(http_requests_total{job="myapp"}[$__rate_interval])`).
    LegendFormat("{{method}} {{status}}").
    Instant(false)  // range query (default)
```

### Loki
```go
loki.NewDataqueryBuilder().
    Expr(`{app="myapp"} |= "error"`).
    LegendFormat("{{level}}")
```

### Testdata (for examples/testing)
```go
testdata.NewQueryBuilder().
    ScenarioId("random_walk").
    SeriesCount(3)
```

## Alert Rule Builder (alerting)

```go
alerting.NewRuleBuilder(title string) *RuleBuilder
```

### Key Methods

| Method | Description |
|--------|-------------|
| `.Condition(s)` | Condition ref (e.g., "A") |
| `.For(s)` | Pending duration (e.g., "5m") |
| `.FolderUID(s)` | Alert folder UID |
| `.RuleGroup(s)` | Rule group name |
| `.Labels(map[string]string)` | Alert labels |
| `.Annotations(map[string]string)` | Summary, description, runbook URL |
| `.ExecErrState(alerting.RuleExecErrState)` | Behavior on execution error |
| `.NoDataState(alerting.RuleNoDataState)` | Behavior when no data |
| `.IsPaused(bool)` | Pause the rule |

### Wrapping for gcx (no convenience Manifest function)

```go
ruleObj, err := rule.Build()
if err != nil {
    // handle error
}

return resource.NewManifestBuilder().
    ApiVersion("notifications.alerting.grafana.app/v0alpha1").
    Kind("AlertRule").
    Metadata(resource.NewMetadataBuilder().Name("rule-name")).
    Spec(ruleObj)
```

## Common Patterns

### Multi-panel dashboard with Prometheus

```go
func MyServiceDashboard() *resource.ManifestBuilder {
    requestRate := dashboard.NewPanelBuilder().
        Title("Request Rate").
        Visualization(timeseries.NewVisualizationBuilder()).
        Data(dashboard.NewQueryGroupBuilder().
            Target(dashboard.NewTargetBuilder().Query(
                prometheus.NewDataqueryBuilder().
                    Expr(`sum(rate(http_requests_total{service="my-svc"}[$__rate_interval])) by (method)`).
                    LegendFormat("{{method}}"),
            )),
        )

    errorRate := dashboard.NewPanelBuilder().
        Title("Error Rate").
        Visualization(stat.NewVisualizationBuilder()).
        Data(dashboard.NewQueryGroupBuilder().
            Target(dashboard.NewTargetBuilder().Query(
                prometheus.NewDataqueryBuilder().
                    Expr(`sum(rate(http_requests_total{service="my-svc",status=~"5.."}[$__rate_interval])) / sum(rate(http_requests_total{service="my-svc"}[$__rate_interval]))`).
                    LegendFormat("error ratio"),
            )),
        )

    builder := dashboard.NewDashboardBuilder("My Service Overview").
        Tags([]string{"team:platform"}).
        Panel("request-rate", requestRate).
        Panel("error-rate", errorRate).
        AutoGridLayout(
            dashboard.AutoGrid().
                WithItem("request-rate").
                WithItem("error-rate"),
        )

    return dashboard.Manifest("my-service-overview", builder)
}
```
