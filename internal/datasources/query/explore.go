package query

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/common/model"
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
	// TableName is the StarTree editor field when the Explore payload includes
	// one. Empty means the Pinot URL builder derives it from Expr.
	TableName string
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

// DatemathFrom converts a PromQL duration string (which permits compound
// units like "1h30m" or "1d12h", per model.ParseDuration) into a Grafana
// datemath "now-<n>s" expression. Datemath's relative-time grammar only
// accepts a single amount+unit pair, so a compound duration must be
// normalized to one unit before it's usable as an Explore link's `from` —
// "now-1h30m" is not valid datemath and silently breaks the link's time
// range. Falls back to the raw "now-<window>" string if window somehow
// isn't a valid PromQL duration (callers validate their own duration flags
// already; this is defense in depth, not the primary check).
func DatemathFrom(window string) string {
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

// ShortExploreRange normalizes the visible Explore time range using a short
// default lookback window when no explicit range was provided.
func ShortExploreRange(from, to string) (string, string) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if to == "" {
		to = "now"
	}
	if from == "" {
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
	maps.Copy(pane, extra)
	return map[string]any{DefaultExplorePaneID: pane}
}

// BuildExploreURL renders the final Grafana Explore URL.
func BuildExploreURL(host string, orgID int64, panes map[string]any, extra map[string]string) string {
	host = strings.TrimRight(strings.TrimSpace(host), "/")
	if host == "" {
		return ""
	}
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
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
