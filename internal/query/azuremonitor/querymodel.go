package azuremonitor

import (
	"fmt"
	"maps"
	"slices"
)

// This file holds the query models sent inside the Grafana datasource query
// API envelope. Both the client (which POSTs them) and the Explore link
// builders in internal/datasources/azuremonitor call these functions, so the
// two shapes cannot drift apart.

// MetricsQueryModel builds the "Azure Monitor" (metrics) query model.
func MetricsQueryModel(dsUID string, req QueryRequest) map[string]any {
	// Sort dimensions so the request body is deterministic; Azure treats the
	// filter list as unordered.
	filters := make([]any, 0, len(req.DimensionFilters))
	for _, dim := range slices.Sorted(maps.Keys(req.DimensionFilters)) {
		filters = append(filters, map[string]any{
			"dimension": dim,
			"operator":  "eq",
			"filters":   []string{req.DimensionFilters[dim]},
		})
	}

	resource := map[string]any{
		"resourceGroup":   req.ResourceGroup,
		"resourceName":    req.ResourceName,
		"metricNamespace": req.MetricNamespace,
	}
	if req.Region != "" {
		resource["region"] = req.Region
	}

	azm := map[string]any{
		"resources":        []any{resource},
		"metricNamespace":  req.MetricNamespace,
		"metricName":       req.MetricName,
		"aggregation":      req.Aggregation,
		"timeGrain":        req.TimeGrain,
		"dimensionFilters": filters,
	}
	if req.Region != "" {
		azm["region"] = req.Region
	}
	if req.Top != "" {
		azm["top"] = req.Top
	}

	return map[string]any{
		"refId":     "A",
		"queryType": "Azure Monitor",
		// The plugin backend builds the ARM URL from the query-level
		// subscription field, not from resources[].subscription. Moving this
		// into the resource entry produces a malformed ARM request
		// (InvalidSubscriptionId).
		"subscription": req.Subscription,
		"datasource": map[string]any{
			"type": pluginID,
			"uid":  dsUID,
		},
		"azureMonitor":  azm,
		"intervalMs":    intervalMsFor(req.Start, req.End),
		"maxDataPoints": maxDataPoints,
	}
}

// LogAnalyticsWorkspaceURI renders the ARM resource URI of a Log Analytics
// workspace, which is how the plugin identifies the query target.
func LogAnalyticsWorkspaceURI(subscription, resourceGroup, workspace string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.OperationalInsights/workspaces/%s",
		subscription, resourceGroup, workspace)
}

// LogsQueryModel builds the "Azure Log Analytics" (KQL) query model.
func LogsQueryModel(dsUID string, req LogsQueryRequest) map[string]any {
	return map[string]any{
		"refId":     "A",
		"queryType": "Azure Log Analytics",
		"datasource": map[string]any{
			"type": pluginID,
			"uid":  dsUID,
		},
		"azureLogAnalytics": map[string]any{
			"resources":    []string{LogAnalyticsWorkspaceURI(req.Subscription, req.ResourceGroup, req.Workspace)},
			"query":        req.Query,
			"resultFormat": "table",
		},
		"intervalMs":    intervalMsFor(req.Start, req.End),
		"maxDataPoints": maxDataPoints,
	}
}

// ResourceGraphQueryModel builds the "Azure Resource Graph" (KQL) query model.
func ResourceGraphQueryModel(dsUID string, req ResourceGraphRequest) map[string]any {
	return map[string]any{
		"refId":     "A",
		"queryType": "Azure Resource Graph",
		"datasource": map[string]any{
			"type": pluginID,
			"uid":  dsUID,
		},
		// Unlike metrics (query-level "subscription", singular), Resource
		// Graph reads a query-level "subscriptions" array.
		"subscriptions": req.Subscriptions,
		"azureResourceGraph": map[string]any{
			"query":        req.Query,
			"resultFormat": "table",
		},
		// Resource Graph results are not time-scoped, so the interval is fixed.
		"intervalMs":    resourceGraphIntervalMs,
		"maxDataPoints": maxDataPoints,
	}
}
