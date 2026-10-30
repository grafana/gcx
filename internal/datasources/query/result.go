package query

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/grafana/gcx/internal/query/tempo"
)

var ErrNoResult = errors.New("query returned no results")

// EmptyResultContext adds enough information to make an enforced empty-result
// failure useful when several diagnostic queries are run together.
type EmptyResultContext struct {
	Expr          string
	DatasourceUID string
	Start         time.Time
	End           time.Time
	Suggestion    string
}

// ErrorOnEmptyWithContext wraps an empty-result failure with the query context
// while preserving errors.Is(err, ErrNoResult) for callers and tests.
func ErrorOnEmptyWithContext(data any, context EmptyResultContext) error {
	err := ErrorOnEmpty(data)
	if !errors.Is(err, ErrNoResult) {
		return err
	}
	rangeText := "unspecified"
	if !context.Start.IsZero() || !context.End.IsZero() {
		rangeText = context.Start.Format(time.RFC3339) + " to " + context.End.Format(time.RFC3339)
	}
	details := "expression: " + context.Expr + "\ndatasource: " + context.DatasourceUID + "\ntime range: " + rangeText
	suggestion := context.Suggestion
	if strings.TrimSpace(suggestion) == "" {
		suggestion = "Run `gcx config check` to verify the datasource configuration, then retry with a wider time range."
	}
	return gcxerrors.DetailedError{
		Summary:     "query returned no results",
		Details:     details,
		Parent:      ErrNoResult,
		Suggestions: []string{suggestion},
	}
}

// ErrorOnEmpty returns ErrNoResult when a supported signal query contains no
// result items. Empty results remain successful unless the command's opt-in
// --error-on-empty flag requests this check.
func ErrorOnEmpty(data any) error {
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
		empty = (resp.Flamegraph == nil || len(resp.Flamegraph.Names) == 0) && !pyroscope.DotHasNodes(resp.Dot)
	default:
		return fmt.Errorf("cannot require a result for unsupported response type %T", data)
	}
	if empty {
		return ErrNoResult
	}
	return nil
}
