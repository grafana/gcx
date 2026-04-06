package adapter_test

import (
	"testing"

	"github.com/grafana/gcx/internal/resources/adapter"
)

func TestSlugifyName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "lowercase passthrough", input: "my-pipeline", want: "my-pipeline"},
		{name: "uppercase to lower", input: "My-Pipeline", want: "my-pipeline"},
		{name: "spaces to hyphens", input: "grafana instance health", want: "grafana-instance-health"},
		{name: "special chars replaced", input: "web_check@v2!", want: "web-check-v2"},
		{name: "consecutive hyphens collapsed", input: "foo---bar", want: "foo-bar"},
		{name: "leading/trailing trimmed", input: "-foo-bar-", want: "foo-bar"},
		{name: "empty returns resource", input: "", want: "resource"},
		{name: "only special chars returns resource", input: "!!!@@@", want: "resource"},
		{name: "numeric passthrough", input: "12345", want: "12345"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := adapter.SlugifyName(tt.input)
			if got != tt.want {
				t.Errorf("SlugifyName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestComposeName(t *testing.T) {
	tests := []struct {
		name string
		slug string
		id   string
		want string
	}{
		{name: "slug and id", slug: "web-check", id: "8127", want: "web-check-8127"},
		{name: "empty slug", slug: "", id: "8127", want: "8127"},
		{name: "empty id", slug: "web-check", id: "", want: "web-check-"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := adapter.ComposeName(tt.slug, tt.id)
			if got != tt.want {
				t.Errorf("ComposeName(%q, %q) = %q, want %q", tt.slug, tt.id, got, tt.want)
			}
		})
	}
}

func TestExtractIDFromSlug(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wantID string
		wantOK bool
	}{
		{name: "slug-id format", input: "web-check-8127", wantID: "8127", wantOK: true},
		{name: "pure numeric", input: "18155", wantID: "18155", wantOK: true},
		{name: "multi-segment slug", input: "grafana-instance-health-5594", wantID: "5594", wantOK: true},
		{name: "name only - no numeric suffix", input: "web-check", wantID: "", wantOK: false},
		{name: "name only - no hyphen", input: "webcheck", wantID: "", wantOK: false},
		{name: "empty string", input: "", wantID: "", wantOK: false},
		{name: "trailing non-numeric", input: "check-abc", wantID: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotOK := adapter.ExtractIDFromSlug(tt.input)
			if gotID != tt.wantID || gotOK != tt.wantOK {
				t.Errorf("ExtractIDFromSlug(%q) = (%q, %v), want (%q, %v)",
					tt.input, gotID, gotOK, tt.wantID, tt.wantOK)
			}
		})
	}
}

func TestExtractInt64IDFromSlug(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wantID int64
		wantOK bool
	}{
		{name: "slug-id format", input: "web-check-8127", wantID: 8127, wantOK: true},
		{name: "pure numeric", input: "5594", wantID: 5594, wantOK: true},
		{name: "no numeric suffix", input: "web-check", wantID: 0, wantOK: false},
		{name: "empty string", input: "", wantID: 0, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotOK := adapter.ExtractInt64IDFromSlug(tt.input)
			if gotID != tt.wantID || gotOK != tt.wantOK {
				t.Errorf("ExtractInt64IDFromSlug(%q) = (%d, %v), want (%d, %v)",
					tt.input, gotID, gotOK, tt.wantID, tt.wantOK)
			}
		})
	}
}
