package tempo_test

import (
	"testing"

	"github.com/grafana/gcx/internal/query/tempo"
)

func TestParseFlatSpansetFilters_Valid(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want []tempo.TraceQLFilter
	}{
		{
			name: "single equality",
			expr: `{ span.http.status_code = 500 }`,
			want: []tempo.TraceQLFilter{{Scope: "span", Tag: "http.status_code", Operator: "=", Value: "500"}},
		},
		{
			name: "quoted string value",
			expr: `{ resource.service.name = "checkout" }`,
			want: []tempo.TraceQLFilter{{Scope: "resource", Tag: "service.name", Operator: "=", Value: "checkout"}},
		},
		{
			name: "multiple ANDed comparisons",
			expr: `{ span.http.status_code = 500 && resource.service.name = "checkout" }`,
			want: []tempo.TraceQLFilter{
				{Scope: "span", Tag: "http.status_code", Operator: "=", Value: "500"},
				{Scope: "resource", Tag: "service.name", Operator: "=", Value: "checkout"},
			},
		},
		{
			name: "not-equal, regex, greater-than, less-than",
			expr: `{ span.foo != "bar" && span.name =~ "GET.*" && span.duration > 100 && span.retries < 3 }`,
			want: []tempo.TraceQLFilter{
				{Scope: "span", Tag: "foo", Operator: "!=", Value: "bar"},
				{Scope: "span", Tag: "name", Operator: "=~", Value: "GET.*"},
				{Scope: "span", Tag: "duration", Operator: ">", Value: "100"},
				{Scope: "span", Tag: "retries", Operator: "<", Value: "3"},
			},
		},
		{
			name: "dotted tag under a single scope",
			expr: `{ resource.k8s.pod.name = "foo-123" }`,
			want: []tempo.TraceQLFilter{{Scope: "resource", Tag: "k8s.pod.name", Operator: "=", Value: "foo-123"}},
		},
		{
			name: "quoted value containing a decoy operator substring",
			expr: `{ resource.service.name = "checkout!=prod" }`,
			want: []tempo.TraceQLFilter{{Scope: "resource", Tag: "service.name", Operator: "=", Value: "checkout!=prod"}},
		},
		{
			name: "quoted value containing a decoy >= substring",
			expr: `{ resource.name = "a>=b" }`,
			want: []tempo.TraceQLFilter{{Scope: "resource", Tag: "name", Operator: "=", Value: "a>=b"}},
		},
		{
			// unquote decodes real LogQL/Go string escapes via
			// strconv.Unquote rather than stripping the backslash and
			// keeping the escaped rune literally — "a\nb" must become an
			// actual newline, not the three characters "a", "n", "b".
			name: "quoted value with escape sequences decodes them",
			expr: `{ resource.name = "a\nb" }`,
			want: []tempo.TraceQLFilter{{Scope: "resource", Tag: "name", Operator: "=", Value: "a\nb"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tempo.ParseFlatSpansetFilters(tt.expr)
			if !ok {
				t.Fatalf("expected ok=true, got false")
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d filters, want %d: %+v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("filter %d: got %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseFlatSpansetFilters_Unsupported(t *testing.T) {
	tests := map[string]string{
		"greater-or-equal unsupported":       `{ span.http.status_code >= 500 }`,
		"less-or-equal unsupported":          `{ span.http.status_code <= 500 }`,
		"or logic unsupported":               `{ span.foo = "bar" || span.baz = "qux" }`,
		"bare intrinsic unsupported":         `{ duration > 500ms }`,
		"scoped intrinsic-style unsupported": `{ status = error }`,
		"nested spanset unsupported":         `{ span.foo = "bar" } && { span.baz = "qux" }`,
		"pipeline stage unsupported":         `{ span.foo = "bar" } | select(span.baz)`,
		"structural operator unsupported":    `{ span.foo = "bar" } >> { span.baz = "qux" }`,
		"unknown scope unsupported":          `{ notascope.foo = "bar" }`,
		"invalid escape falls back":          `{ resource.name = "a\zb" }`,
		"empty spanset unsupported":          `{ }`,
		"missing braces unsupported":         `span.foo = "bar"`,
	}

	for name, expr := range tests {
		t.Run(name, func(t *testing.T) {
			if _, ok := tempo.ParseFlatSpansetFilters(expr); ok {
				t.Fatalf("expected ok=false for %q", expr)
			}
		})
	}
}
