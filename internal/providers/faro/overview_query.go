package faro

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/grafana/gcx/internal/query/loki"
	"golang.org/x/sync/errgroup"
)

// The LogQL below mirrors the Frontend Observability plugin's own overview
// queries (app-o11y-kwl: src/scenes/components/apps/overview/queries.ts).
// All run as instant queries; the `[window]` range vector embedded in each
// expression is what scopes them to --since, exactly like the plugin's
// `[$__auto]`. The stream selector keys on `app_id` (the numeric Faro app
// id), which is why callers must resolve a name to its id first.

// topExceptionsLimit caps the number of distinct exceptions surfaced in the
// snapshot. The plugin uses topk(10); we keep the same query and trim for
// display so the describe view stays compact.
const topExceptionsLimit = 10

// pageLoadsExpr counts page loads. A measurement line carrying ` ttfb=` is
// the plugin's proxy for "one page load" (TTFB is emitted once per load).
func pageLoadsExpr(appID, window string) string {
	return fmt.Sprintf(`sum(count_over_time({app_id=%q,kind="measurement"} |= " ttfb=" | logfmt [%s]))`, appID, window)
}

// errorsExpr counts exception events over the window.
func errorsExpr(appID, window string) string {
	return fmt.Sprintf(`sum(count_over_time({app_id=%q,kind="exception"} | logfmt [%s]))`, appID, window)
}

// vitalExpr is the p75 of a single Core Web Vital. The line filter narrows to
// lines carrying the field before logfmt+unwrap so series without it don't
// dilute the quantile.
func vitalExpr(appID, field, window string) string {
	return fmt.Sprintf(`quantile_over_time(0.75, {app_id=%q,kind="measurement"} |= %q | logfmt | unwrap %s [%s])`,
		appID, field+"=", field, window)
}

// topErrorsExpr returns the most frequent exceptions grouped by type+message,
// matching the plugin's top-exceptions breakdown (trimmed to (type, value)).
func topErrorsExpr(appID, window string) string {
	return fmt.Sprintf(`topk(%d, sum by (type, value) (count_over_time({app_id=%q,kind="exception"} | logfmt [%s])))`,
		topExceptionsLimit, appID, window)
}

// fetchOverview runs every KPI query in parallel and folds the responses into
// one Overview. Window is a PromQL/LogQL duration literal (already validated).
func fetchOverview(ctx context.Context, client *loki.Client, datasourceUID, appID, appName, window string) (*Overview, error) {
	specs := vitalSpecs()

	var pageResp, errResp, topResp *loki.MetricQueryResponse
	vitalResps := make([]*loki.MetricQueryResponse, len(specs))

	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		resp, err := queryInstant(egCtx, client, datasourceUID, pageLoadsExpr(appID, window))
		if err != nil {
			return fmt.Errorf("page-loads query failed: %w", err)
		}
		pageResp = resp
		return nil
	})
	eg.Go(func() error {
		resp, err := queryInstant(egCtx, client, datasourceUID, errorsExpr(appID, window))
		if err != nil {
			return fmt.Errorf("errors query failed: %w", err)
		}
		errResp = resp
		return nil
	})
	eg.Go(func() error {
		resp, err := queryInstant(egCtx, client, datasourceUID, topErrorsExpr(appID, window))
		if err != nil {
			return fmt.Errorf("top-errors query failed: %w", err)
		}
		topResp = resp
		return nil
	})
	for i, spec := range specs {
		eg.Go(func() error {
			resp, err := queryInstant(egCtx, client, datasourceUID, vitalExpr(appID, spec.field, window))
			if err != nil {
				return fmt.Errorf("%s query failed: %w", spec.name, err)
			}
			vitalResps[i] = resp
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}

	pageLoads, hasPageLoads := instantScalar(pageResp)
	errCount, hasErr := instantScalar(errResp)

	vitals := make([]WebVital, len(specs))
	for i, spec := range specs {
		v, ok := instantScalar(vitalResps[i])
		vitals[i] = WebVital{Name: spec.name, Unit: spec.unit, P75: v, HasData: ok}
		if ok {
			vitals[i].Rating = spec.rate(v)
		}
	}

	o := &Overview{
		AppID:        appID,
		AppName:      appName,
		Window:       window,
		PageLoads:    pageLoads,
		HasPageLoads: hasPageLoads,
		Errors:       errCount,
		// When there are page loads we know the error count (0 if no
		// exception series came back), so report it rather than "-".
		HasErrors:    hasErr || (hasPageLoads && pageLoads > 0),
		ErrorPercent: computeErrorPercent(errCount, pageLoads),
		WebVitals:    vitals,
		TopErrors:    parseTopErrors(topResp, topExceptionsLimit),
	}
	return o, nil
}

// queryInstant runs a metric LogQL expression as an instant query. The
// expression's own `[window]` range vector determines the lookback, so no
// Start/End is set (an empty range => instant, evaluated at now).
func queryInstant(ctx context.Context, client *loki.Client, datasourceUID, expr string) (*loki.MetricQueryResponse, error) {
	return client.MetricQuery(ctx, datasourceUID, loki.QueryRequest{Query: expr})
}

// computeErrorPercent returns errors as a percentage of page loads. Returns 0
// when there's no traffic (a percentage would be meaningless / divide-by-zero).
func computeErrorPercent(errors, pageLoads float64) float64 {
	if pageLoads <= 0 {
		return 0
	}
	return errors / pageLoads * 100
}

// instantScalar pulls the first sample's value out of a metric LogQL instant
// response. The second return is false when there is no series, the value is
// NaN/Inf, or it can't be parsed — the caller renders those as "no data"
// rather than a misleading 0.
func instantScalar(resp *loki.MetricQueryResponse) (float64, bool) {
	if resp == nil || len(resp.Data.Result) == 0 {
		return 0, false
	}
	return sampleValue(resp.Data.Result[0])
}

func sampleValue(s loki.MetricQuerySample) (float64, bool) {
	if len(s.Value) < 2 {
		return 0, false
	}
	str, ok := s.Value[1].(string)
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(str, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// parseTopErrors turns the top-exceptions response into a sorted slice. Each
// series' (type, value) labels carry the error type and message.
func parseTopErrors(resp *loki.MetricQueryResponse, limit int) []TopError {
	if resp == nil {
		return nil
	}
	out := make([]TopError, 0, len(resp.Data.Result))
	for _, s := range resp.Data.Result {
		v, ok := sampleValue(s)
		if !ok || v <= 0 {
			continue
		}
		out = append(out, TopError{
			Type:    s.Metric["type"],
			Message: s.Metric["value"],
			Count:   int64(v),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}
