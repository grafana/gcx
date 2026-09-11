package loki_test

import (
	"testing"

	"github.com/grafana/gcx/internal/query/loki"
)

func TestExtractStreamSelectors(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want []string
	}{
		{
			name: "bare selector",
			expr: `{app="backstage"}`,
			want: []string{`{app="backstage"}`},
		},
		{
			name: "selector with line filter",
			expr: `{app="backstage"} |= "error"`,
			want: []string{`{app="backstage"}`},
		},
		{
			name: "metric expression wrapping a range vector",
			expr: `rate({app="backstage"}[5m])`,
			want: []string{`{app="backstage"}`},
		},
		{
			name: "aggregation wrapping a metric expression",
			expr: `sum by (app) (rate({app="backstage"}[5m]))`,
			want: []string{`{app="backstage"}`},
		},
		{
			name: "label value containing a closing brace",
			expr: `{msg="value}"} |= "x"`,
			want: []string{`{msg="value}"}`},
		},
		{
			name: "two selectors combined with a binary operator",
			expr: `count_over_time({app="a"}[5m]) + count_over_time({app="b"}[5m])`,
			want: []string{`{app="a"}`, `{app="b"}`},
		},
		{
			name: "duplicate selectors are deduplicated",
			expr: `count_over_time({app="a"}[5m]) / count_over_time({app="a"}[1h])`,
			want: []string{`{app="a"}`},
		},
		{
			// Regression: a brace inside a quoted line-filter string must
			// not be misread as a second selector once scanning resumes
			// past the real one.
			name: "line filter with a quoted brace is not mistaken for a second selector",
			expr: `{app="x"} |= "payload {foo}"`,
			want: []string{`{app="x"}`},
		},
		{
			name: "multiple line filters each containing quoted braces",
			expr: `{app="x"} |= "{a}" |= "{b}"`,
			want: []string{`{app="x"}`},
		},
		{
			name: "quoted brace inside a parser stage argument",
			expr: `{app="x"} | logfmt | line_format "{unrelated}"`,
			want: []string{`{app="x"}`},
		},
		{
			// Regression: LogQL backtick (raw) strings are a second quoting
			// delimiter alongside double quotes. A brace inside one must not
			// be misread as a selector, the same way a double-quoted one
			// isn't.
			name: "regex filter with a backtick string containing a brace",
			expr: "{app=\"x\"} |~ `foo{bar}`",
			want: []string{`{app="x"}`},
		},
		{
			name: "line filter with a backtick string containing a brace",
			expr: "{app=\"x\"} |= `payload {foo}`",
			want: []string{`{app="x"}`},
		},
		{
			// The closing brace of the selector itself is backtick-quoted;
			// it must not be mistaken for the selector's own closing brace.
			name: "selector whose label value is a backtick string containing a brace",
			expr: "{name!~`foo}bar`}",
			want: []string{"{name!~`foo}bar`}"},
		},
		{
			name: "line_format template with braces in a backtick string",
			expr: "{app=\"x\"} | line_format `{{.x}}`",
			want: []string{`{app="x"}`},
		},
		{
			name: "a literal backtick inside a double-quoted string does not open a raw string",
			expr: "{app=\"x\"} |= \"literal ` backtick\" |= `raw {z}`",
			want: []string{`{app="x"}`},
		},
		{
			name: "no selector",
			expr: `vector(1)`,
			want: nil,
		},
		{
			name: "empty expression",
			expr: ``,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := loki.ExtractStreamSelectors(tt.expr)
			if len(got) != len(tt.want) {
				t.Fatalf("ExtractStreamSelectors(%q) = %v, want %v", tt.expr, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ExtractStreamSelectors(%q)[%d] = %q, want %q", tt.expr, i, got[i], tt.want[i])
				}
			}
		})
	}
}
