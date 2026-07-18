package faro_test

import (
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/faro"
	"github.com/grafana/gcx/internal/query/loki"
)

func TestOverviewExprBuilders(t *testing.T) {
	const (
		appID  = "153"
		window = "1h"
	)
	tests := []struct {
		name     string
		got      string
		contains []string
	}{
		{
			name: "page loads",
			got:  faro.PageLoadsExpr(appID, window),
			contains: []string{
				`{app_id="153",kind="measurement"}`,
				`|= " ttfb="`,
				`count_over_time(`,
				`[1h]`,
			},
		},
		{
			name: "errors",
			got:  faro.ErrorsExpr(appID, window),
			contains: []string{
				`{app_id="153",kind="exception"}`,
				`count_over_time(`,
				`[1h]`,
			},
		},
		{
			name: "vital",
			got:  faro.VitalExpr(appID, "lcp", window),
			contains: []string{
				`quantile_over_time(0.75,`,
				`{app_id="153",kind="measurement"}`,
				`|= "lcp="`,
				`| unwrap lcp [1h]`,
			},
		},
		{
			name: "top errors",
			got:  faro.TopErrorsExpr(appID, window),
			contains: []string{
				`topk(10,`,
				`sum by (type, value)`,
				`{app_id="153",kind="exception"}`,
				`[1h]`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, want := range tt.contains {
				if !strings.Contains(tt.got, want) {
					t.Errorf("expr %q\n  missing substring %q", tt.got, want)
				}
			}
		})
	}
}

func TestVitalSpecRate(t *testing.T) {
	tests := []struct {
		vital string
		value float64
		want  faro.WebVitalRating
	}{
		{"LCP", 2000, faro.RatingGood},
		{"LCP", 2500, faro.RatingGood}, // boundary: <= goodMax is good
		{"LCP", 3000, faro.RatingNeedsImprovement},
		{"LCP", 4000, faro.RatingNeedsImprovement}, // boundary: == poorMin is not yet poor
		{"LCP", 4500, faro.RatingPoor},
		{"CLS", 0.05, faro.RatingGood},
		{"CLS", 0.2, faro.RatingNeedsImprovement},
		{"CLS", 0.3, faro.RatingPoor},
		{"INP", 150, faro.RatingGood},
		{"TTFB", 1000, faro.RatingNeedsImprovement},
	}
	for _, tt := range tests {
		got, ok := faro.RateVital(tt.vital, tt.value)
		if !ok {
			t.Fatalf("unknown vital %q", tt.vital)
		}
		if got != tt.want {
			t.Errorf("%s rate(%v) = %q, want %q", tt.vital, tt.value, got, tt.want)
		}
	}
}

func TestInstantScalar(t *testing.T) {
	tests := []struct {
		name    string
		resp    *loki.MetricQueryResponse
		wantVal float64
		wantOK  bool
	}{
		{name: "nil", resp: nil, wantOK: false},
		{name: "empty", resp: &loki.MetricQueryResponse{}, wantOK: false},
		{
			name: "value present",
			resp: &loki.MetricQueryResponse{Data: loki.MetricQueryData{Result: []loki.MetricQuerySample{
				{Value: []any{1.0, "42.5"}},
			}}},
			wantVal: 42.5,
			wantOK:  true,
		},
		{
			name: "non-string value",
			resp: &loki.MetricQueryResponse{Data: loki.MetricQueryData{Result: []loki.MetricQuerySample{
				{Value: []any{1.0, 42.5}},
			}}},
			wantOK: false,
		},
		{
			name: "NaN",
			resp: &loki.MetricQueryResponse{Data: loki.MetricQueryData{Result: []loki.MetricQuerySample{
				{Value: []any{1.0, "NaN"}},
			}}},
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := faro.InstantScalar(tt.resp)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got != tt.wantVal {
				t.Errorf("val = %v, want %v", got, tt.wantVal)
			}
		})
	}
}

func TestParseTopErrors(t *testing.T) {
	resp := &loki.MetricQueryResponse{Data: loki.MetricQueryData{Result: []loki.MetricQuerySample{
		{Metric: map[string]string{"type": "TypeError", "value": "a"}, Value: []any{1.0, "5"}},
		{Metric: map[string]string{"type": "RangeError", "value": "b"}, Value: []any{1.0, "12"}},
		{Metric: map[string]string{"value": "c"}, Value: []any{1.0, "0"}},   // dropped: zero
		{Metric: map[string]string{"value": "d"}, Value: []any{1.0, "bad"}}, // dropped: unparseable
	}}}

	got := faro.ParseTopErrors(resp, 10)
	if len(got) != 2 {
		t.Fatalf("got %d errors, want 2", len(got))
	}
	if got[0].Message != "b" || got[0].Count != 12 {
		t.Errorf("expected highest-count error first, got %+v", got[0])
	}
	if got[1].Type != "TypeError" || got[1].Count != 5 {
		t.Errorf("unexpected second error: %+v", got[1])
	}

	limited := faro.ParseTopErrors(resp, 1)
	if len(limited) != 1 || limited[0].Message != "b" {
		t.Errorf("limit not applied: %+v", limited)
	}
}

func TestComputeErrorPercent(t *testing.T) {
	if got := faro.ComputeErrorPercent(5, 0); got != 0 {
		t.Errorf("zero denominator = %v, want 0", got)
	}
	if got := faro.ComputeErrorPercent(5, 100); got != 5 {
		t.Errorf("5/100 = %v, want 5", got)
	}
}

func TestFormatVital(t *testing.T) {
	tests := []struct {
		name string
		v    faro.WebVital
		want string
	}{
		{"no data", faro.WebVital{Name: "LCP", Unit: "ms", HasData: false}, "-"},
		{"ms sub-second", faro.WebVital{Unit: "ms", P75: 250, HasData: true, Rating: faro.RatingGood}, "250ms  good"},
		{"ms over a second", faro.WebVital{Unit: "ms", P75: 2100, HasData: true, Rating: faro.RatingNeedsImprovement}, "2.10s  needs-improvement"},
		{"cls score", faro.WebVital{Unit: "score", P75: 0.045, HasData: true, Rating: faro.RatingGood}, "0.045  good"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := faro.FormatVital(tt.v); got != tt.want {
				t.Errorf("formatVital() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatErrorCount(t *testing.T) {
	if got := faro.FormatErrorCount(0, 0, false, false); got != "-" {
		t.Errorf("no errors = %q, want -", got)
	}
	if got := faro.FormatErrorCount(3, 0, true, false); got != "3" {
		t.Errorf("no traffic = %q, want 3", got)
	}
	if got := faro.FormatErrorCount(3, 7.5, true, true); got != "3 (7.50% of loads)" {
		t.Errorf("with traffic = %q", got)
	}
}

func TestAppLabel(t *testing.T) {
	if got := faro.AppLabel("shop", "153"); got != "shop (id 153)" {
		t.Errorf("name+id = %q", got)
	}
	if got := faro.AppLabel("", "153"); got != "id 153" {
		t.Errorf("id only = %q", got)
	}
	if got := faro.AppLabel("shop", ""); got != "shop" {
		t.Errorf("name only = %q", got)
	}
}
