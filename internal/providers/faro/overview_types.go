package faro

// This file defines the data model for `gcx frontend overview`: a headless
// mirror of the Frontend Observability plugin's per-app overview page. The
// KPI shape follows the plugin's own Loki queries (see overview_query.go):
// page loads + error count + the five Core Web Vitals (p75) + top exceptions.

// WebVitalRating is Google's Core Web Vitals bucket for a p75 measurement.
type WebVitalRating string

const (
	// RatingGood means the p75 value is within the "good" threshold.
	RatingGood WebVitalRating = "good"
	// RatingNeedsImprovement means the p75 value is between good and poor.
	RatingNeedsImprovement WebVitalRating = "needs-improvement"
	// RatingPoor means the p75 value exceeds the "poor" threshold.
	RatingPoor WebVitalRating = "poor"
)

// WebVital is the p75 of one Core Web Vital over the requested window.
// Unit is "ms" for the timing vitals and "score" for CLS (which is unitless).
type WebVital struct {
	Name    string         `json:"name"`
	P75     float64        `json:"p75"`
	Unit    string         `json:"unit"`
	Rating  WebVitalRating `json:"rating,omitempty"`
	HasData bool           `json:"hasData"`
}

// TopError is one row of the top-exceptions breakdown, grouped by error
// type + message the same way the plugin's overview table groups them.
type TopError struct {
	Type    string `json:"type,omitempty"`
	Message string `json:"message"`
	Count   int64  `json:"count"`
}

// Overview is the full KPI snapshot for one Frontend Observability app.
type Overview struct {
	AppID        string     `json:"appId"`
	AppName      string     `json:"appName"`
	Window       string     `json:"window"`
	PageLoads    float64    `json:"pageLoads"`
	HasPageLoads bool       `json:"-"`
	Errors       float64    `json:"errors"`
	HasErrors    bool       `json:"-"`
	ErrorPercent float64    `json:"errorPercent"`
	WebVitals    []WebVital `json:"webVitals"`
	TopErrors    []TopError `json:"topErrors,omitempty"`
}

// HasTraffic reports whether any page loads were observed in the window.
// When false, the snapshot is rendered but the command exits non-zero so
// callers can branch on $? (mirrors `appo11y services get`).
func (o *Overview) HasTraffic() bool {
	return o.HasPageLoads && o.PageLoads > 0
}

// vitalSpec describes one Core Web Vital: the logfmt field name carrying its
// value, the display unit, and the Google p75 good/poor thresholds.
type vitalSpec struct {
	name    string
	field   string
	unit    string
	goodMax float64 // p75 <= goodMax => good
	poorMin float64 // p75 >  poorMin => poor
}

// vitalSpecs returns the five Core Web Vitals in the order the plugin's
// overview surfaces them (LCP/INP/CLS as the core trio, then FCP/TTFB).
// Timing thresholds are in milliseconds — the unit the Faro Web SDK emits
// for these measurements; CLS is a unitless layout-shift score.
func vitalSpecs() []vitalSpec {
	return []vitalSpec{
		{name: "LCP", field: "lcp", unit: "ms", goodMax: 2500, poorMin: 4000},
		{name: "INP", field: "inp", unit: "ms", goodMax: 200, poorMin: 500},
		{name: "CLS", field: "cls", unit: "score", goodMax: 0.1, poorMin: 0.25},
		{name: "FCP", field: "fcp", unit: "ms", goodMax: 1800, poorMin: 3000},
		{name: "TTFB", field: "ttfb", unit: "ms", goodMax: 800, poorMin: 1800},
	}
}

// rate buckets a p75 value into a Core Web Vitals rating.
func (s vitalSpec) rate(p75 float64) WebVitalRating {
	switch {
	case p75 <= s.goodMax:
		return RatingGood
	case p75 > s.poorMin:
		return RatingPoor
	default:
		return RatingNeedsImprovement
	}
}
