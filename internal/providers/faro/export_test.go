package faro

import "github.com/grafana/gcx/internal/query/loki"

// Test helpers — expose the unexported overview internals so the external
// faro_test package can exercise them as white-box units.

// PageLoadsExpr exposes pageLoadsExpr for testing.
func PageLoadsExpr(appID, window string) string { return pageLoadsExpr(appID, window) }

// ErrorsExpr exposes errorsExpr for testing.
func ErrorsExpr(appID, window string) string { return errorsExpr(appID, window) }

// VitalExpr exposes vitalExpr for testing.
func VitalExpr(appID, field, window string) string { return vitalExpr(appID, field, window) }

// TopErrorsExpr exposes topErrorsExpr for testing.
func TopErrorsExpr(appID, window string) string { return topErrorsExpr(appID, window) }

// InstantScalar exposes instantScalar for testing.
func InstantScalar(resp *loki.MetricQueryResponse) (float64, bool) { return instantScalar(resp) }

// ParseTopErrors exposes parseTopErrors for testing.
func ParseTopErrors(resp *loki.MetricQueryResponse, limit int) []TopError {
	return parseTopErrors(resp, limit)
}

// ComputeErrorPercent exposes computeErrorPercent for testing.
func ComputeErrorPercent(errors, pageLoads float64) float64 {
	return computeErrorPercent(errors, pageLoads)
}

// FormatVital exposes formatVital for testing.
func FormatVital(v WebVital) string { return formatVital(v) }

// FormatErrorCount exposes formatErrorCount for testing.
func FormatErrorCount(errs, pct float64, hasErrors, hasTraffic bool) string {
	return formatErrorCount(errs, pct, hasErrors, hasTraffic)
}

// AppLabel exposes appLabel for testing.
func AppLabel(name, id string) string { return appLabel(name, id) }

// RateVital looks up the Core Web Vital spec by name and returns the rating
// it assigns to value — lets tests cover the thresholds without exporting
// the vitalSpec type.
func RateVital(name string, value float64) (WebVitalRating, bool) {
	for _, s := range vitalSpecs() {
		if s.name == name {
			return s.rate(value), true
		}
	}
	return "", false
}
