package pyroscope_test

import (
	"reflect"
	"testing"

	"github.com/grafana/gcx/internal/query/pyroscope"
)

func TestParseLabelSelector(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want []pyroscope.LabelMatcher
	}{
		{
			name: "empty selector",
			expr: `{}`,
			want: nil,
		},
		{
			name: "single matcher",
			expr: `{service_name="frontend"}`,
			want: []pyroscope.LabelMatcher{{Key: "service_name", Operator: "=", Value: "frontend"}},
		},
		{
			name: "multiple matchers, all operators",
			expr: `{service_name="frontend", env!="staging", region=~"us-.*", pod!~"canary-.*"}`,
			want: []pyroscope.LabelMatcher{
				{Key: "service_name", Operator: "=", Value: "frontend"},
				{Key: "env", Operator: "!=", Value: "staging"},
				{Key: "region", Operator: "=~", Value: "us-.*"},
				{Key: "pod", Operator: "!~", Value: "canary-.*"},
			},
		},
		{
			// unquote decodes real escape sequences via strconv.Unquote
			// rather than stripping the backslash and keeping the escaped
			// rune literally — "a\nb" must become an actual newline, not
			// the three characters "a", "n", "b".
			name: "matcher value with escape sequences decodes them",
			expr: `{service_name="a\nb"}`,
			want: []pyroscope.LabelMatcher{{Key: "service_name", Operator: "=", Value: "a\nb"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := pyroscope.ParseLabelSelector(tt.expr)
			if !ok {
				t.Fatalf("expected ok=true, got false")
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseLabelSelector_Unsupported(t *testing.T) {
	tests := map[string]string{
		"missing braces":  `service_name="frontend"`,
		"malformed value": `{service_name=frontend}`,
		"empty key":       `{="frontend"}`,
		"invalid escape":  `{service_name="a\zb"}`,
	}

	for name, expr := range tests {
		t.Run(name, func(t *testing.T) {
			if _, ok := pyroscope.ParseLabelSelector(expr); ok {
				t.Fatalf("expected ok=false for %q", expr)
			}
		})
	}
}
