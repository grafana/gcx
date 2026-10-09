package services

import (
	"github.com/grafana/gcx/internal/datasources/prometheus"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
)

// ServiceLinks are Grafana Explore deep links for the PromQL queries behind
// a services get RED snapshot. Fields are omitted when a link can't be
// built (e.g. no datasource UID resolved).
type ServiceLinks struct {
	Rate       string `json:"rate,omitempty"        yaml:"rate,omitempty"`
	Errors     string `json:"errors,omitempty"      yaml:"errors,omitempty"`
	LatencyP95 string `json:"latency_p95,omitempty" yaml:"latency_p95,omitempty"`
}

// buildServiceLinks wraps prometheus.QueryExploreURL for the three RED
// expressions behind a services get snapshot. Returns nil (not a struct
// with all-empty fields) when grafanaURL or datasourceUID is missing, so
// the `links` JSON key disappears entirely rather than serializing as an
// empty object.
func buildServiceLinks(grafanaURL, datasourceUID string, orgID int64, window string, rateExpr, errorExpr, p95Expr string) *ServiceLinks {
	if grafanaURL == "" || datasourceUID == "" {
		return nil
	}
	from := dsquery.DatemathFrom(window)
	build := func(expr string) string {
		return prometheus.QueryExploreURL(grafanaURL, dsquery.ExploreQuery{
			DatasourceUID:  datasourceUID,
			DatasourceType: "prometheus",
			Expr:           expr,
			From:           from,
			To:             "now",
			Instant:        false,
			OrgID:          orgID,
		})
	}
	return &ServiceLinks{
		Rate:       build(rateExpr),
		Errors:     build(errorExpr),
		LatencyP95: build(p95Expr),
	}
}
