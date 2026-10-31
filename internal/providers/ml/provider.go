package ml

import (
	"context"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Providers register once at startup.
	providers.Register(adapter.NewProvider("ml", "Manage Machine Learning forecasts, holidays, and outlier detectors", providers.LoadGrafanaDeps,
		jobs(), holidays(), outliers(),
	).WithCommands(commands))
}

func resource[T adapter.ResourceNamer](kind, plural, path string, example T) adapter.Resource[T] {
	return adapter.Resource[T]{
		Group: "ml.ext.grafana.app", Version: "v1alpha1", Kind: kind, Plural: plural,
		NaturalKey:       "name",
		NaturalKeyPolicy: adapter.NaturalKeyPolicy{RequireUnique: true, IDLabel: "server ID"},
		StripFields:      []string{"id", "grafanaUrl", "datasourceId"}, Example: &example,
		NewClient: func(_ context.Context, deps adapter.ClientDeps) (any, error) {
			return &client[T]{deps: deps, path: path}, nil
		},
	}
}

func jobs() adapter.Resource[Job] {
	return resource("Job", "jobs", "jobs", Job{Name: "request-forecast", Metric: "request_forecast", DatasourceUID: "prometheus", DatasourceType: "prometheus", QueryParams: map[string]any{"expr": "sum(rate(http_requests_total[5m]))"}, Interval: 300, TrainingWindow: 7776000, TrainingFrequency: 86400, Algorithm: "grafana_prophet_1_0_1"})
}
func holidays() adapter.Resource[Holiday] {
	zone, url := "UTC", "https://example.com/holidays.ics"
	return resource("Holiday", "holidays", "holidays", Holiday{Name: "public-holidays", ICalURL: &url, ICalTimeZone: &zone, Jobs: []string{}})
}
func outliers() adapter.Resource[OutlierDetector] {
	return resource("OutlierDetector", "outlierdetectors", "outliers", OutlierDetector{Name: "request-outliers", Metric: "request_outlier", DatasourceUID: "prometheus", DatasourceType: "prometheus", QueryParams: map[string]any{"expr": "sum by (instance) (rate(http_requests_total[5m]))"}, Interval: 300, Algorithm: OutlierAlgorithm{Name: "mad", Sensitivity: 0.5}})
}

func commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}
	cmd := &cobra.Command{Use: "ml", Short: "Manage Machine Learning forecasts, holidays, and outlier detectors.", Long: "Manage Grafana Cloud Machine Learning resources through the ML plugin proxy. Uses the active Grafana credential. Use resources push and resources pull for manifests. Alerts require a parent job or outlier detector ID."}
	loader.BindFlags(cmd.PersistentFlags())
	jobCmd := resourceCommands(loader, "jobs", "forecast job", "forecast jobs", jobs())
	outlierCmd := resourceCommands(loader, "outlier-detectors", "outlier detector", "outlier detectors", outliers())
	addAlertCommands(jobCmd, loader, "jobs")
	addAlertCommands(outlierCmd, loader, "outliers")
	cmd.AddCommand(jobCmd, resourceCommands(loader, "holidays", "holiday", "holidays", holidays()), outlierCmd)
	return []*cobra.Command{cmd}
}
