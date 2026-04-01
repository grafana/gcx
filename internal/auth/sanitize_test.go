package auth

import "testing"

func TestStripControlChars(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text unchanged", "access_denied", "access_denied"},
		{"strips ANSI escape", "bad\x1b[31m error", "bad[31m error"},
		{"strips newline and tab", "line1\nline2\ttab", "line1line2tab"},
		{"strips null byte", "before\x00after", "beforeafter"},
		{"strips DEL", "del\x7fchar", "delchar"},
		{"preserves unicode", "héllo wörld", "héllo wörld"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripControlChars(tt.input)
			if got != tt.want {
				t.Errorf("stripControlChars(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
