package query

import (
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/grafana/gcx/internal/query/tempo"
)

var ErrNoResult = errors.New("query returned no results")

// RequireResult returns ErrNoResult when a supported signal query contains no
// result items. Empty results remain successful unless the command's opt-in
// --require-result flag requests this check.
func RequireResult(data any) error {
	var empty bool
	switch resp := data.(type) {
	case *prometheus.QueryResponse:
		empty = len(resp.Data.Result) == 0
	case *loki.QueryResponse:
		empty = len(resp.Data.Result) == 0
	case *loki.MetricQueryResponse:
		empty = len(resp.Data.Result) == 0
	case *tempo.SearchResponse:
		empty = len(resp.Traces) == 0
	case *tempo.MetricsResponse:
		empty = len(resp.Series) == 0
	case *pyroscope.QueryResponse:
		empty = (resp.Flamegraph == nil || len(resp.Flamegraph.Names) == 0) && resp.Dot == ""
	default:
		return fmt.Errorf("cannot require a result for unsupported response type %T", data)
	}
	if empty {
		return ErrNoResult
	}
	return nil
}
