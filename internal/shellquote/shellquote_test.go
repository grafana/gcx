package shellquote_test

import (
	"testing"

	"github.com/grafana/gcx/internal/shellquote"
)

func TestQuote(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty string is quoted", in: "", want: "''"},
		{name: "safe token untouched", in: "my-cluster", want: "my-cluster"},
		{name: "safe token with flag-like chars untouched", in: "--format=json", want: "--format=json"},
		{name: "space forces quoting", in: "up == 1", want: "'up == 1'"},
		{name: "single quote escaped in canonical POSIX form", in: "it's", want: `'it'\''s'`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shellquote.Quote(tt.in); got != tt.want {
				t.Errorf("Quote(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestEscape(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "safe token still quoted", in: "my-cluster", want: "'my-cluster'"},
		{name: "single quote escaped", in: "it's-a-cluster", want: `'it'\''s-a-cluster'`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shellquote.Escape(tt.in); got != tt.want {
				t.Errorf("Escape(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestJoin(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{name: "safe tokens joined without quotes", in: []string{"gcx", "resources", "get", "--format=json"}, want: "gcx resources get --format=json"},
		{name: "spaced value quoted", in: []string{"gcx", "resources", "get", "--format", "up == 1"}, want: "gcx resources get --format 'up == 1'"},
		{name: "embedded single quote escaped", in: []string{"echo", "it's"}, want: `echo 'it'\''s'`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shellquote.Join(tt.in); got != tt.want {
				t.Errorf("Join(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
