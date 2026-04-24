package query

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const DefaultExplorePaneID = "gcx"

// ExploreQuery describes the query state encoded into a Grafana Explore URL.
type ExploreQuery struct {
	DatasourceUID  string
	DatasourceType string
	Expr           string
	From           string
	To             string
	Instant        bool
	Step           time.Duration
	OrgID          int64
}

// ExploreRange normalizes the visible Explore time range.
func ExploreRange(from, to string, instant bool) (string, string) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if to == "" {
		to = "now"
	}
	if from == "" {
		if instant {
			from = "now-1m"
		} else {
			from = "now-1h"
		}
	}
	return from, to
}

// ShortExploreRange normalizes the visible Explore time range using a short
// default lookback window when no explicit range was provided.
func ShortExploreRange(from, to string) (string, string) {
	from, to = ExploreRange(from, to, false)
	if strings.TrimSpace(from) == "now-1h" {
		from = "now-1m"
	}
	return from, to
}

// ExploreDatasource renders the datasource reference block expected by Explore.
func ExploreDatasource(dsType, uid string) map[string]any {
	return map[string]any{
		"type": dsType,
		"uid":  uid,
	}
}

// SinglePane renders a single Explore pane payload under the default pane ID.
func SinglePane(datasourceUID string, queries []any, from, to string, extra map[string]any) map[string]any {
	pane := map[string]any{
		"datasource": datasourceUID,
		"queries":    queries,
		"range": map[string]any{
			"from": from,
			"to":   to,
		},
	}
	for key, value := range extra {
		pane[key] = value
	}
	return map[string]any{DefaultExplorePaneID: pane}
}

// BuildExploreURL renders the final Grafana Explore URL.
func BuildExploreURL(host string, orgID int64, panes map[string]any, extra map[string]string) string {
	host = strings.TrimRight(strings.TrimSpace(host), "/")
	if host == "" {
		return ""
	}

	panesJSON, err := json.Marshal(panes)
	if err != nil {
		return ""
	}

	u, err := url.Parse(host + "/explore")
	if err != nil {
		return ""
	}

	params := u.Query()
	params.Set("schemaVersion", "1")
	params.Set("panes", string(panesJSON))
	if orgID > 0 {
		params.Set("orgId", strconv.FormatInt(orgID, 10))
	}
	for key, value := range extra {
		params.Set(key, value)
	}
	u.RawQuery = params.Encode()

	return u.String()
}
