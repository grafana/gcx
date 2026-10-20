package loki //nolint:testpackage // white-box: exercises the unexported maskQuoted helper directly

import "testing"

func TestMaskQuoted(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want string
	}{
		{
			name: "no quotes",
			expr: `{app=x}`,
			want: `{app=x}`,
		},
		{
			name: "double-quoted value blanked, delimiters kept",
			expr: `{app="foo"}`,
			want: `{app="   "}`,
		},
		{
			name: "backtick-quoted value blanked, delimiters kept",
			expr: "`foo`",
			want: "`   `",
		},
		{
			name: "escaped quote inside double quotes stays masked",
			expr: `"a\"b"`,
			want: `"    "`,
		},
		{
			name: "backtick inside double quotes does not open a raw string",
			expr: "\"a`b\"",
			want: "\"   \"",
		},
		{
			name: "length is preserved",
			expr: `{app="foo"} |= "bar"`,
			want: `{app="   "} |= "   "`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maskQuoted(tt.expr)
			if got != tt.want {
				t.Errorf("maskQuoted(%q) = %q, want %q", tt.expr, got, tt.want)
			}
			if len(got) != len(tt.expr) {
				t.Errorf("maskQuoted(%q) changed length: got %d, want %d", tt.expr, len(got), len(tt.expr))
			}
		})
	}
}
