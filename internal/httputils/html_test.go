package httputils_test

import (
	"testing"

	"github.com/grafana/gcx/internal/httputils"
)

func TestIsHTML(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		want        bool
	}{
		{name: "declared text/html", contentType: "text/html; charset=UTF-8", body: "oops", want: true},
		{name: "declared xhtml", contentType: "application/xhtml+xml", body: "", want: true},
		{name: "sniffed without content type", body: "  \n<!DOCTYPE html><html></html>", want: true},
		{name: "sniffed behind a generic type", contentType: "application/octet-stream", body: "<html><body>x</body></html>", want: true},
		{name: "JSON", contentType: "application/json", body: `{"message":"x"}`},
		{name: "plain text", contentType: "text/plain", body: "plugin not found"},
		{name: "empty", contentType: "", body: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := httputils.IsHTML(tt.contentType, []byte(tt.body)); got != tt.want {
				t.Errorf("IsHTML(%q, %q) = %v, want %v", tt.contentType, tt.body, got, tt.want)
			}
		})
	}
}
