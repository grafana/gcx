package reports

import (
	"context"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/client-go/rest"
)

// ReportResource is shared by provider commands and the generic resource pipeline.
func ReportResource() adapter.Resource[Report] {
	return adapter.Resource[Report]{
		Group: "slo.ext.grafana.app", Version: "v1alpha1", Kind: Kind,
		StripFields: []string{"uuid"},
		Example: &Report{
			UUID: "my-report", Name: "Weekly availability", Description: "Availability of the selected SLOs",
			TimeSpan:         "weeklySundayToSunday",
			ReportDefinition: ReportDefinition{Slos: []ReportSlo{{SloUUID: "my-slo"}}},
		},
		NewClient: func(_ context.Context, deps adapter.ClientDeps) (any, error) {
			return &Client{restConfig: config.NamespacedRESTConfig{Config: rest.Config{Host: deps.BaseURL}}, httpClient: deps.HTTP}, nil
		},
	}
}
