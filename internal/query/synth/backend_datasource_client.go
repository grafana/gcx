package synth

// Named queries are the second, distinct way to talk to the Synthetic Monitoring
// datasource. The rest of this package proxies the SM REST API (checks, probes)
// through the plugin's `sm` routes; this file queries the plugin's Go *backend*,
// which owns the PromQL and LogQL for check telemetry.
//
// The contract is deliberately thin: gcx sends a query *name* plus parameters and
// receives Grafana data frames. It never sends an expression, never names a
// Prometheus or Loki datasource, and never learns which metric backs a number.
// That is what keeps `gcx` and the SM app reporting the same values -- the
// definition lives in exactly one place, the plugin's query registry.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/dataframe"
	"github.com/grafana/gcx/internal/query/grafanaquery"
	"github.com/grafana/gcx/internal/queryerror"
	"k8s.io/client-go/rest"
)

// DatasourceType is the plugin id of the SM datasource, as it appears in a query
// request. It is the wire spelling; internal/datasources/query.NormalizeKind maps
// it to the shorter "synthetic-monitoring" used for config keys.
const DatasourceType = "synthetic-monitoring-datasource"

// namedRefID is the single refId gcx uses. One query per request keeps error
// handling unambiguous: the backend answers per refId, so a batch would need the
// caller to reason about partial success.
const namedRefID = "A"

// NamedQuery is a request for a query the SM backend knows by name, such as
// "checks_uptime". Params are the arguments that query declares (job, instance,
// frequency, probe, ...); the backend validates them and rejects what it does not
// recognise, so gcx does not duplicate that validation.
type NamedQuery struct {
	Name   string
	Params map[string]any
}

// NamedResult is the outcome of a named query: the frames the backing datasource
// produced, passed through untouched, plus the expression the backend built.
//
// ExecutedQuery exists for transparency, not for reuse. Showing a user what ran
// is useful; feeding it back as an expression would reintroduce the query
// knowledge this design removes from clients.
type NamedResult struct {
	Frames        []dataframe.Frame
	ExecutedQuery string
}

// BackendDatasourceClient queries the SM backend datasource by query name.
type BackendDatasourceClient struct {
	queryClient *grafanaquery.Client
}

// NewBackendDatasourceClient creates a named-query client using the caller's Grafana
// credential from the REST config.
// This client communicates directly with the backend datasource for the synthetic monitoring app.
func NewBackendDatasourceClient(cfg config.NamespacedRESTConfig) (*BackendDatasourceClient, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	return &BackendDatasourceClient{queryClient: grafanaquery.NewClientWithHTTPClient(cfg, httpClient)}, nil
}

// Query asks the SM datasource identified by datasourceUID for q over [from, to].
func (c *BackendDatasourceClient) Query(
	ctx context.Context,
	datasourceUID string,
	q NamedQuery,
	from, to time.Time,
) (*NamedResult, error) {
	if q.Name == "" {
		return nil, errors.New("a query name is required")
	}
	if datasourceUID == "" {
		return nil, errors.New("a synthetic monitoring datasource uid is required")
	}

	body, err := json.Marshal(map[string]any{
		"from":    strconv.FormatInt(from.UnixMilli(), 10),
		"to":      strconv.FormatInt(to.UnixMilli(), 10),
		"queries": []any{buildQuery(datasourceUID, q)},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	respBody, err := c.queryClient.Execute(ctx, body, DatasourceType, q.Name)
	if err != nil {
		return nil, err
	}

	var resp dataframe.Response
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	result, ok := resp.Results[namedRefID]
	if !ok {
		return nil, fmt.Errorf("query %q: response contained no result", q.Name)
	}

	// A rejected query -- unknown name, missing parameter, or a user who may not
	// query the backing datasource -- arrives here rather than as an HTTP error.
	if result.Error != "" {
		status := result.Status
		if status == 0 {
			status = http.StatusBadRequest
		}

		return nil, queryerror.New("synthetic monitoring", q.Name, status, result.Error, result.ErrorSource)
	}

	return &NamedResult{
		Frames:        result.Frames,
		ExecutedQuery: executedQuery(result.Frames),
	}, nil
}

// buildQuery assembles one query object. The parameters are written first so the
// envelope fields overwrite them: a caller must not be able to retarget the query
// at another datasource by passing a "datasource" parameter.
func buildQuery(datasourceUID string, q NamedQuery) map[string]any {
	query := make(map[string]any, len(q.Params)+3)
	maps.Copy(query, q.Params)

	query["refId"] = namedRefID
	query["queryType"] = q.Name
	query["datasource"] = map[string]any{
		"type": DatasourceType,
		"uid":  datasourceUID,
	}

	return query
}

// executedQuery returns the first expression the backend reported building.
func executedQuery(frames []dataframe.Frame) string {
	for i := range frames {
		if meta := frames[i].Schema.Meta; meta != nil && meta.ExecutedQueryString != "" {
			return meta.ExecutedQueryString
		}
	}

	return ""
}

// Mean averages the numeric field of the first frame, reporting false when there
// is nothing to average.
//
// This mirrors how the app reduces a range frame to the single percentage on a
// check card. It is a stopgap: while the reduction lives in the client, gcx and
// the app can drift even though they share the expression, so the backend should
// grow a reduced form of these queries and this helper should then go away.
func Mean(res *NamedResult) (float64, bool) {
	if res == nil || len(res.Frames) == 0 {
		return 0, false
	}

	values := numericValues(res.Frames[0])

	var sum float64
	var n int
	for _, v := range values {
		// Gaps come back as nulls. Treating them as zero would report a check as
		// failing when it simply was not scraped.
		f, ok := v.(float64)
		if !ok {
			continue
		}
		sum += f
		n++
	}

	if n == 0 {
		return 0, false
	}

	return sum / float64(n), true
}

// HasNumericField reports whether res contains a field typed "number" -- the
// shape Mean can reduce. A log-backed query (e.g. check_error_logs) returns
// string/label fields and no numeric field, so this is false for it -- that
// case is "not reducible", distinct from a metric query that legitimately
// returned no points.
func HasNumericField(res *NamedResult) bool {
	if res == nil || len(res.Frames) == 0 {
		return false
	}

	for _, field := range res.Frames[0].Schema.Fields {
		if field.Type == "number" {
			return true
		}
	}

	return false
}

// numericValues returns the values of the first field typed "number", which is
// where Prometheus and Loki put the numbers. Deliberately narrower than "the
// first non-time field": a log frame's first non-time field is a string (the
// log line), and treating that as a numeric series would silently produce
// wrong sums rather than a clear "not reducible" signal.
func numericValues(frame dataframe.Frame) []any {
	for i, field := range frame.Schema.Fields {
		if field.Type != "number" || i >= len(frame.Data.Values) {
			continue
		}

		return frame.Data.Values[i]
	}

	return nil
}
