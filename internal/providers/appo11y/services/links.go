package services

import (
	"fmt"
	"time"

	"github.com/grafana/gcx/internal/datasources/prometheus"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/prometheus/common/model"
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
	from := datemathFrom(window)
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

// datemathFrom converts a `--since`-style PromQL duration (which permits
// compound units like "1h30m" or "1d12h", per model.ParseDuration) into a
// Grafana datemath "now-<n>s" expression. Datemath's relative-time grammar
// only accepts a single amount+unit pair, so a compound duration must be
// normalized to one unit before it's usable in an Explore link's `from`
// parameter — "now-1h30m" is not valid datemath and silently breaks the
// link's time range. Falls back to the raw "now-<window>" string if window
// somehow isn't a valid PromQL duration (callers validate --since already;
// this is defense in depth, not the primary check).
func datemathFrom(window string) string {
	d, err := model.ParseDuration(window)
	if err != nil {
		return "now-" + window
	}
	seconds := int64(time.Duration(d).Round(time.Second) / time.Second)
	if seconds <= 0 {
		seconds = 1
	}
	return fmt.Sprintf("now-%ds", seconds)
}
